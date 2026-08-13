package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type pathOptions struct {
	Trim                  bool
	ExpandTilde           bool
	HomeDir               string
	StripAtPrefix         bool
	NormalizeUnicodeSpaces bool
}

var unicodeSpacesRe = regexp.MustCompile("[\u00a0\u2000-\u200a\u202f\u205f\u3000]")

// normalizeWindowsShellPath converts Git Bash, MSYS, Cygwin, and WSL drive
// paths to a form native Windows APIs accept.
func normalizeWindowsShellPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return p
	}
	m := regexp.MustCompile(`^/(?:mnt/|cygdrive/)?([a-z])(?:/(.*))?$`).FindStringSubmatch(p)
	if m == nil {
		return p
	}
	suffix := strings.ReplaceAll(m[2], "/", "\\")
	return strings.ToUpper(m[1]) + ":\\" + suffix
}

func normalizePath(input string, options pathOptions) string {
	normalized := input
	if options.Trim {
		normalized = strings.TrimSpace(normalized)
	}
	if options.NormalizeUnicodeSpaces {
		normalized = unicodeSpacesRe.ReplaceAllString(normalized, " ")
	}
	if options.StripAtPrefix && strings.HasPrefix(normalized, "@") {
		normalized = normalized[1:]
	}
	if os.PathSeparator == '\\' {
		normalized = normalizeWindowsShellPath(normalized)
	}

	if options.ExpandTilde || true {
		home := options.HomeDir
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if normalized == "~" {
			return home
		}
		if strings.HasPrefix(normalized, "~/") || (os.PathSeparator == '\\' && strings.HasPrefix(normalized, `~\`)) {
			return filepath.Join(home, normalized[2:])
		}
	}

	if strings.HasPrefix(normalized, "file://") {
		p, err := fileURLToPath(normalized)
		if err == nil {
			return p
		}
	}

	return normalized
}

func fileURLToPath(rawURL string) (string, error) {
	rest := strings.TrimPrefix(rawURL, "file://")
	if strings.HasPrefix(rest, "/") && strings.Contains(rest, ":") {
		// file:///C:/foo -> C:\foo
		rest = rest[1:]
	}
	rest = strings.ReplaceAll(rest, "/", string(os.PathSeparator))
	return rest, nil
}

func isAbsolute(p string) bool {
	return filepath.IsAbs(p)
}

// EnvCwd is the environment variable that overrides the tool working
// directory. When unset, tools operate in the process working directory.
const EnvCwd = "TAU_TOOL_CWD"

// workDir returns the effective working directory: TAU_TOOL_CWD if set,
// otherwise the process working directory.
func workDir() string {
	if d := os.Getenv(EnvCwd); d != "" {
		return normalizePath(d, pathOptions{})
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// resolveToCwd resolves a path relative to the effective working directory,
// handling ~ expansion and absolute paths.
func resolveToCwd(filePath string) string {
	return resolvePath(filePath, workDir())
}

func resolvePath(input, baseDir string) string {
	normalized := normalizePath(input, pathOptions{NormalizeUnicodeSpaces: true, StripAtPrefix: true})
	normalizedBase := normalizePath(baseDir, pathOptions{})
	if isAbsolute(normalized) {
		return filepath.Clean(normalized)
	}
	return filepath.Clean(filepath.Join(normalizedBase, normalized))
}

// pathExists reports whether a file or directory exists.
func pathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

const narrowNoBreakSpace = "\u202f"

func tryMacOSScreenshotPath(p string) string {
	re := regexp.MustCompile(` (AM|PM)\.`)
	return re.ReplaceAllString(p, narrowNoBreakSpace+"$1.")
}

func tryNFDVariant(p string) string {
	return normalizeNFD(p)
}

// normalizeNFD is a no-op placeholder for macOS NFD (decomposed) form
// normalization. macOS stores filenames in NFD form; on other platforms the
// variant simply never matches, so no transformation is needed.
func normalizeNFD(p string) string {
	return p
}

func tryCurlyQuoteVariant(p string) string {
	return strings.ReplaceAll(p, "'", "\u2019")
}

// resolveReadPath resolves a path, trying macOS filename variants (AM/PM
// narrow no-break space, NFD normalization, curly apostrophes) if the direct
// path does not exist.
func resolveReadPath(filePath string) string {
	resolved := resolveToCwd(filePath)
	if pathExists(resolved) {
		return resolved
	}

	amPmVariant := tryMacOSScreenshotPath(resolved)
	if amPmVariant != resolved && pathExists(amPmVariant) {
		return amPmVariant
	}

	nfdVariant := tryNFDVariant(resolved)
	if nfdVariant != resolved && pathExists(nfdVariant) {
		return nfdVariant
	}

	curlyVariant := tryCurlyQuoteVariant(resolved)
	if curlyVariant != resolved && pathExists(curlyVariant) {
		return curlyVariant
	}

	nfdCurlyVariant := tryCurlyQuoteVariant(nfdVariant)
	if nfdCurlyVariant != resolved && pathExists(nfdCurlyVariant) {
		return nfdCurlyVariant
	}

	return resolved
}
