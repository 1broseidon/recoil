package mine

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	DefaultMaxFileBytes  = 256 * 1024
	DefaultMaxChunkChars = 3000
)

type Options struct {
	Path          string
	SourceRoot    string
	IncludeHidden bool
	MaxFileBytes  int64
	MaxChunkChars int
	MaxChunks     int
}

type File struct {
	Path string
	Rel  string
	Size int64
}

type Chunk struct {
	SourcePath string
	SourceRef  string
	Content    string
	Index      int
	StartLine  int
	EndLine    int
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

	var files []File
	var skipped []Skip
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			skipped = append(skipped, Skip{Path: displayPath(path, sourceRoot), Reason: walkErr.Error()})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name(), opts.IncludeHidden) {
				skipped = append(skipped, Skip{Path: displayPath(path, sourceRoot), Reason: "skipped directory"})
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			skipped = append(skipped, Skip{Path: displayPath(path, sourceRoot), Reason: "symlink"})
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			skipped = append(skipped, Skip{Path: displayPath(path, sourceRoot), Reason: err.Error()})
			return nil
		}
		file, skip := fileFromInfo(path, sourceRoot, info, opts)
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
	return ChunkText(file.Rel, text, opts.MaxChunkChars), nil
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
	name := info.Name()
	if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
		return File{}, Skip{Path: rel, Reason: "hidden file"}
	}
	if !isSupportedTextPath(name) {
		return File{}, Skip{Path: rel, Reason: "unsupported extension"}
	}
	if info.Size() > opts.MaxFileBytes {
		return File{}, Skip{Path: rel, Reason: "file too large"}
	}
	return File{Path: path, Rel: rel, Size: info.Size()}, Skip{}
}

func displayPath(path, sourceRoot string) string {
	if rel, err := filepath.Rel(sourceRoot, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func shouldSkipDir(name string, includeHidden bool) bool {
	if !includeHidden && strings.HasPrefix(name, ".") {
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
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".txt", ".text", ".rst", ".adoc", ".asciidoc", ".org":
		return true
	default:
		return false
	}
}
