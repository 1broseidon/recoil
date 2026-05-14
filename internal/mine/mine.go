package mine

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/1broseidon/recoil/internal/pathmatch"
	"github.com/1broseidon/recoil/internal/sourcequality"
)

const ignoreFileName = ".recoilignore"

const (
	DefaultMaxFileBytes  = 256 * 1024
	DefaultMaxChunkChars = 3000
)

type Options struct {
	Path                     string
	SourceRoot               string
	IncludeHidden            bool
	IncludeHiddenOperational bool
	FollowRepoSymlinks       bool
	PolicyConfigured         bool
	IncludePaths             []string
	ExcludePaths             []string
	MaxFileBytes             int64
	MaxChunkChars            int
	MaxChunks                int
}

type File struct {
	Path    string
	Rel     string
	Size    int64
	ModTime string
}

type Chunk struct {
	SourcePath string
	SourceRef  string
	Content    string
	Index      int
	StartLine  int
	EndLine    int
	FileHash   string
	FileSize   int64
	FileMTime  string
}

type Result struct {
	Root         string
	FilesScanned int
	FilesSkipped int
	Chunks       []Chunk
	Skipped      []Skip
}

type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func Collect(opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	files, root, skipped, err := Discover(opts)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Root:         root,
		FilesScanned: len(files),
		FilesSkipped: len(skipped),
		Skipped:      skipped,
	}
	for _, file := range files {
		chunks, err := ChunksForFile(file, opts)
		if err != nil {
			result.FilesSkipped++
			result.Skipped = append(result.Skipped, Skip{Path: file.Rel, Reason: err.Error()})
			continue
		}
		for _, chunk := range chunks {
			result.Chunks = append(result.Chunks, chunk)
			if opts.MaxChunks > 0 && len(result.Chunks) >= opts.MaxChunks {
				return result, nil
			}
		}
	}
	return result, nil
}

func Discover(opts Options) ([]File, string, []Skip, error) {
	opts = normalizeOptions(opts)
	root, err := cleanDirOrFile(opts.Path)
	if err != nil {
		return nil, "", nil, err
	}
	sourceRoot := opts.SourceRoot
	if sourceRoot == "" {
		sourceRoot = root
	}
	sourceRoot, _ = filepath.Abs(sourceRoot)
	if real, err := filepath.EvalSymlinks(sourceRoot); err == nil {
		sourceRoot = real
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, "", nil, err
	}
	if !info.IsDir() {
		file, skip := fileFromInfo(root, sourceRoot, info, opts)
		if skip.Reason != "" {
			return nil, filepath.Dir(root), []Skip{skip}, nil
		}
		return []File{file}, filepath.Dir(root), nil, nil
	}

	rules, err := loadIgnoreFile(filepath.Join(root, ignoreFileName))
	if err != nil {
		return nil, "", nil, fmt.Errorf("read %s: %w", ignoreFileName, err)
	}

	var files []File
	var skipped []Skip
	err = filepath.WalkDir(root, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: walkErr.Error()})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = entry.Name()
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if shouldSkipDir(rel, entry.Name(), opts.IncludeHidden) {
				skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: "skipped directory"})
				return filepath.SkipDir
			}
			if rules.matchDir(rel) {
				skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: "recoilignore"})
				return filepath.SkipDir
			}
			if hasIgnoreSentinel(p) {
				skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: "recoilignore sentinel"})
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if !opts.FollowRepoSymlinks {
				skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: "symlink"})
				return nil
			}
			file, skip := fileFromSymlink(p, root, sourceRoot, rel, opts)
			if skip.Reason != "" {
				skipped = append(skipped, skip)
				return nil
			}
			files = append(files, file)
			return nil
		}
		if entry.Name() == ignoreFileName {
			return nil
		}
		if rules.matchFile(rel) {
			skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: "recoilignore"})
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			skipped = append(skipped, Skip{Path: displayPath(p, sourceRoot), Reason: err.Error()})
			return nil
		}
		file, skip := fileFromInfo(p, sourceRoot, info, opts)
		if skip.Reason != "" {
			skipped = append(skipped, skip)
			return nil
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, "", nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, root, skipped, nil
}

func ChunksForFile(file File, opts Options) ([]Chunk, error) {
	opts = normalizeOptions(opts)
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("binary content")
	}
	text := string(data)
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid utf-8")
	}
	chunks := ChunkText(file.Rel, text, opts.MaxChunkChars)
	hash := sha256.Sum256(data)
	for i := range chunks {
		chunks[i].FileHash = hex.EncodeToString(hash[:])
		chunks[i].FileSize = file.Size
		chunks[i].FileMTime = file.ModTime
	}
	return chunks, nil
}

func ChunkText(sourcePath, text string, maxChars int) []Chunk {
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkChars
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	var chunks []Chunk
	var b strings.Builder
	startLine := 1
	currentLine := 1
	flush := func(endLine int) {
		content := strings.TrimSpace(b.String())
		if content == "" {
			b.Reset()
			startLine = currentLine
			return
		}
		index := len(chunks) + 1
		chunks = append(chunks, Chunk{
			SourcePath: sourcePath,
			SourceRef:  fmt.Sprintf("chunk %d lines %d-%d", index, startLine, endLine),
			Content:    content,
			Index:      index,
			StartLine:  startLine,
			EndLine:    endLine,
		})
		b.Reset()
		startLine = currentLine
	}

	for i, line := range lines {
		if line == "" && i == len(lines)-1 {
			continue
		}
		lineNo := currentLine
		nextLine := currentLine
		if strings.HasSuffix(line, "\n") {
			nextLine++
		} else if line != "" {
			nextLine++
		}
		if b.Len() > 0 && b.Len()+len(line) > maxChars {
			flush(lineNo - 1)
		}
		if b.Len() == 0 {
			startLine = lineNo
		}
		b.WriteString(line)
		currentLine = nextLine
	}
	flush(currentLine - 1)
	return chunks
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = "."
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	if opts.MaxChunkChars <= 0 {
		opts.MaxChunkChars = DefaultMaxChunkChars
	}
	if !opts.PolicyConfigured {
		// Preserve zero-value Options behavior for callers that construct
		// Options directly instead of going through the CLI config loader.
		opts.IncludeHiddenOperational = true
		opts.FollowRepoSymlinks = true
	}
	return opts
}

func cleanDirOrFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

func fileFromInfo(path, sourceRoot string, info fs.FileInfo, opts Options) (File, Skip) {
	rel := displayPath(path, sourceRoot)
	return fileFromInfoWithRel(path, rel, info, opts)
}

func fileFromInfoWithRel(path, rel string, info fs.FileInfo, opts Options) (File, Skip) {
	if matchesAnyPath(rel, opts.ExcludePaths) {
		return File{}, Skip{Path: rel, Reason: "configured exclude"}
	}
	forcedInclude := matchesAnyPath(rel, opts.IncludePaths)
	if !opts.IncludeHidden && !forcedInclude && isHiddenPath(rel) &&
		(!opts.IncludeHiddenOperational || !sourcequality.OperationalHiddenPath(rel)) {
		return File{}, Skip{Path: rel, Reason: "hidden file"}
	}
	if !forcedInclude && !isSupportedTextPath(rel) {
		return File{}, Skip{Path: rel, Reason: "unsupported extension"}
	}
	if info.Size() > opts.MaxFileBytes {
		return File{}, Skip{Path: rel, Reason: "file too large"}
	}
	return File{
		Path:    path,
		Rel:     rel,
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}, Skip{}
}

func fileFromSymlink(path, root, sourceRoot, rel string, opts Options) (File, Skip) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return File{}, Skip{Path: displayPath(path, sourceRoot), Reason: "symlink"}
	}
	if !pathWithin(root, target) {
		return File{}, Skip{Path: displayPath(path, sourceRoot), Reason: "symlink outside root"}
	}
	info, err := os.Stat(target)
	if err != nil {
		return File{}, Skip{Path: displayPath(path, sourceRoot), Reason: err.Error()}
	}
	if info.IsDir() {
		return File{}, Skip{Path: displayPath(path, sourceRoot), Reason: "symlink directory"}
	}
	return fileFromInfoWithRel(target, rel, info, opts)
}

func displayPath(path, sourceRoot string) string {
	if rel, err := filepath.Rel(sourceRoot, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

type ignoreRules struct {
	dirPrefixes []string
	globs       []string
}

func loadIgnoreFile(path string) (ignoreRules, error) {
	var rules ignoreRules
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return rules, nil
		}
		return rules, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "./")
		if strings.HasSuffix(line, "/") {
			rules.dirPrefixes = append(rules.dirPrefixes, strings.TrimSuffix(line, "/"))
			continue
		}
		rules.globs = append(rules.globs, line)
	}
	return rules, scanner.Err()
}

func (r ignoreRules) matchDir(rel string) bool {
	rel = strings.TrimSuffix(rel, "/")
	for _, prefix := range r.dirPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	for _, glob := range r.globs {
		if matched, _ := path.Match(glob, rel); matched {
			return true
		}
	}
	return false
}

func (r ignoreRules) matchFile(rel string) bool {
	for _, prefix := range r.dirPrefixes {
		if strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	for _, glob := range r.globs {
		if matched, _ := path.Match(glob, rel); matched {
			return true
		}
		if matched, _ := path.Match(glob, path.Base(rel)); matched {
			return true
		}
	}
	return false
}

func hasIgnoreSentinel(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ignoreFileName))
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "*" || line == "**" || line == "/" {
			return true
		}
	}
	return false
}

func shouldSkipDir(rel, name string, includeHidden bool) bool {
	if !includeHidden && strings.HasPrefix(name, ".") && !isOperationalHiddenDir(rel) {
		return true
	}
	switch name {
	case ".brainfile", ".git", ".hg", ".svn", ".recoil",
		"node_modules", "vendor", "dist", "build", "out", "target",
		"coverage", ".next", ".cache", "tmp", "temp":
		return true
	default:
		return false
	}
}

func isSupportedTextPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if isOperationalTextBase(base) {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".txt", ".text", ".rst", ".adoc", ".asciidoc", ".org":
		return true
	default:
		return false
	}
}

func pathWithin(root, target string) bool {
	root, _ = filepath.Abs(root)
	target, _ = filepath.Abs(target)
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isHiddenPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func isOperationalHiddenDir(rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	return rel == ".github" ||
		rel == ".well-known" ||
		strings.HasSuffix(rel, "/.well-known")
}

func isOperationalTextBase(base string) bool {
	// Match the basename minus extension exactly. A prefix match would have
	// accepted noise like developers_projects.yml or contributing_authors.csv.
	name := strings.TrimSuffix(base, filepath.Ext(base))
	switch name {
	case "agents", "claude", "readme",
		"security", "security_contacts", "security_policy",
		"contributing", "contribute",
		"developers", "developing", "developer":
		return true
	}
	return false
}

func matchesAnyPath(rel string, patterns []string) bool {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		pattern = strings.TrimPrefix(pattern, "./")
		if pattern == "" {
			continue
		}
		if pathmatch.Match(pattern, rel) {
			return true
		}
	}
	return false
}
