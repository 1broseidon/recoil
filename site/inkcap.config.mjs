/* Everything inkcap needs to print this site. The rest of the build is
 * github.com/1broseidon/inkcap, shared with the other chain.sh manuals. */
export default {
  name: 'recoil',
  url: 'https://recoil.sh',
  repo: '1broseidon/recoil',
  tagline: 'local-first memory for coding agents',
  built: 'Built in Go on SQLite FTS5',
  accent: {
    light: { accent: '#7D3A66', soft: '#F2E4EC' },
    dark: { accent: '#D18AB5', soft: '#2B1A25' },
    terminal: { prompt: '#C57FA8', key: '#E0A6C7' },
  },
  /* The Quickstart's console blocks are captures: `inkcap build` re-runs
   * their `$ recoil …` lines against ./recoil (`make build` first) in a
   * fresh /tmp/orbit seeded from site/fixture/, with its own HOME, and
   * rewrites the outputs in MANUAL.md. `git diff` is the review. Pages
   * builds have no binary and print what is committed. */
  captures: {
    cwd: '/tmp/orbit',
    path: '..',
    setup: ['git init -q'],
  },
}
