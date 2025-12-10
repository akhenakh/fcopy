package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"golang.design/x/clipboard"
)

// isExcluded checks if a given path matches any of the glob patterns.
func isExcluded(path string, excludePatterns []string) (bool, string) {
	if len(excludePatterns) == 0 {
		return false, ""
	}
	// Use ToSlash for consistent matching across OSes, as glob patterns use '/'
	pathToCheck := filepath.ToSlash(path)

	for _, pattern := range excludePatterns {
		matched, err := filepath.Match(pattern, pathToCheck)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid exclude pattern '%s': %v\n", pattern, err)
			continue
		}
		if matched {
			return true, pattern
		}
	}
	return false, ""
}

// estimateTokens provides a more detailed heuristic for token counting.
// It classifies runes and applies different weights.
func estimateTokens(content string) (int, string) {
	if content == "" {
		return 0, ""
	}

	var wordChars, spaceChars, symbolChars, otherChars int

	for _, r := range content {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			wordChars++
		} else if unicode.IsSpace(r) {
			spaceChars++
		} else if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			symbolChars++
		} else {
			otherChars++
		}
	}

	// Heuristics refined based on real-world tokenizer (e.g., LLaMA) feedback.
	// - Words: ~4 chars per token (standard English approximation). Remains a solid baseline.
	wordTokens := wordChars / 4
	// - Whitespace: Tokenizers are very efficient with whitespace (indentation, newlines).
	//   Using a higher divisor to be more conservative.
	spaceTokens := spaceChars / 5
	// - Symbols: Many symbols are combined into single tokens (e.g., '->', ':=', '!=')
	//   or merged with words. This ratio reflects that not every symbol is a new token.
	symbolTokens := symbolChars * 2 / 3
	// - Other: Penalize unknown/multi-byte chars as they likely become multiple tokens.
	otherTokens := otherChars * 2

	totalEstimate := wordTokens + spaceTokens + symbolTokens + otherTokens

	details := fmt.Sprintf("~%d tokens (from %dk words, %dk whitespace, %dk symbols)",
		totalEstimate,
		(wordChars+500)/1000,
		(spaceChars+500)/1000,
		(symbolChars+500)/1000,
	)
	if otherChars > 0 {
		details = fmt.Sprintf("~%d tokens (from %dk words, %dk whitespace, %dk symbols, %d other)",
			totalEstimate,
			(wordChars+500)/1000,
			(spaceChars+500)/1000,
			(symbolChars+500)/1000,
			otherChars,
		)
	}

	return totalEstimate, details
}

// getRepoName extracts a readable repository name from a URL to use as the base directory name.
func getRepoName(url string) string {
	// Simple heuristic: take the last part of the URL and strip .git
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) == 0 {
		return "repo"
	}
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".git")
	if name == "" {
		return "repo"
	}
	return name
}

// target represents a file system location to process
type target struct {
	absPath     string
	displayBase string
	isDir       bool
}

func main() {
	// Define flags
	promptPtr := flag.String("p", "", "A prompt to append after the main file contents")
	followUpFilePtr := flag.String("f", "", "Path to a file whose content will be appended after the prompt, formatted as markdown")
	outputFilePtr := flag.String("o", "", "Output to the specified file instead of clipboard")
	stdoutPtr := flag.Bool("s", false, "Output to stdout instead of clipboard")
	termCopyPtr := flag.Bool("t", false, "Use terminal-aware clipboard (OSC 52, kitty), ideal for SSH")
	excludePatternsPtr := flag.String("x", "", "Comma-separated list of glob patterns to exclude (e.g., '.git,*.log,dist/*')")
	gitRepoPtr := flag.String("g", "", "Git repository URL to clone and process (shallow clone)") // New flag

	// Custom usage message
	flag.Usage = func() {
		progName := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <path1> [path2 ...]\n", progName)
		fmt.Fprintf(os.Stderr, "Processes files, directories, or git repositories, formats them as markdown.\n")
		fmt.Fprintf(os.Stderr, "\nArguments:\n")
		fmt.Fprintf(os.Stderr, "  <path1> [path2 ...]  Paths to files or directories to process.\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s internal/ README.md\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -g https://github.com/user/repo\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Refactor this\" main.go\n", progName)
	}

	flag.Parse()

	// Check for mutually exclusive output options
	if *stdoutPtr && *outputFilePtr != "" {
		fmt.Fprintf(os.Stderr, "Error: -s (stdout) and -o (output file) options are mutually exclusive.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Parse exclude patterns
	var excludePatterns []string
	if *excludePatternsPtr != "" {
		patterns := strings.Split(*excludePatternsPtr, ",")
		for _, p := range patterns {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				excludePatterns = append(excludePatterns, trimmed)
			}
		}
	}

	argPaths := flag.Args()

	// Validate we have something to do
	if len(argPaths) == 0 && *gitRepoPtr == "" && *promptPtr == "" && *followUpFilePtr == "" {
		flag.Usage()
		os.Exit(1)
	}

	var outputBuilder strings.Builder
	var targetsToProcess []target

	// Handle Git Repository if -g is provided
	if *gitRepoPtr != "" {
		// Check if git is installed
		if _, err := exec.LookPath("git"); err != nil {
			log.Fatal("Error: 'git' command not found in PATH. Required for -g flag.")
		}

		// Create temp directory
		tempDir, err := os.MkdirTemp("", "fcopy-git-*")
		if err != nil {
			log.Fatalf("Error creating temporary directory: %v", err)
		}
		defer func() {
			fmt.Fprintf(os.Stderr, "Cleaning up temp directory: %s\n", tempDir)
			os.RemoveAll(tempDir)
		}()

		repoURL := *gitRepoPtr
		fmt.Fprintf(os.Stderr, "Cloning %s into temporary directory...\n", repoURL)

		// git clone --depth 1 <url> <tempDir>
		cmd := exec.Command("git", "clone", "--depth", "1", repoURL, tempDir)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Error cloning repository: %v", err)
		}

		// Add the temp dir to our targets list.
		// We set displayBase to the repository name so paths look like "repo/main.go" instead of "/tmp/123/main.go"
		repoName := getRepoName(repoURL)
		targetsToProcess = append(targetsToProcess, target{
			absPath:     tempDir,
			displayBase: repoName,
			isDir:       true,
		})
	}

	// Handle standard positional arguments
	for _, argPath := range argPaths {
		absPath, err := filepath.Abs(argPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for %s: %v\n", argPath, err)
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error stating path %s: %v\n", argPath, err)
			continue
		}

		var displayBase string
		if filepath.IsAbs(argPath) {
			displayBase = filepath.Clean(argPath)
		} else {
			displayBase = argPath
		}

		targetsToProcess = append(targetsToProcess, target{
			absPath:     absPath,
			displayBase: displayBase,
			isDir:       info.IsDir(),
		})
	}

	// Process all targets (Git repo and/or local paths)
	for _, t := range targetsToProcess {
		// Pre-check exclude for the root path itself (mostly for local args)
		// For git, we usually want to process the root temp dir, but individual files inside will be checked.
		if !strings.HasPrefix(t.absPath, os.TempDir()) { // Don't exclude the temp dir itself
			if excluded, pattern := isExcluded(filepath.ToSlash(filepath.Clean(t.displayBase)), excludePatterns); excluded {
				fmt.Fprintf(os.Stderr, "Skipping path %s (matches exclude pattern '%s')\n", t.displayBase, pattern)
				continue
			}
		}

		if t.isDir {
			processDirectory(t.absPath, t.displayBase, &outputBuilder, excludePatterns)
		} else {
			processFile(t.absPath, t.displayBase, &outputBuilder)
		}
	}

	// Append the prompt from -p if provided
	promptText := *promptPtr
	if promptText != "" {
		if outputBuilder.Len() > 0 {
			outputBuilder.WriteString("\n\n")
		}
		outputBuilder.WriteString(promptText)
		fmt.Fprintf(os.Stderr, "Appended prompt text.\n")
	}

	// Append content from the -f file if provided
	followUpFilePath := *followUpFilePtr
	if followUpFilePath != "" {
		absFollowUpPath, err := filepath.Abs(followUpFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for follow-up file -f %s: %v\n", followUpFilePath, err)
		} else {
			info, err := os.Stat(absFollowUpPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error stating follow-up file -f %s: %v\n", followUpFilePath, err)
			} else if info.IsDir() {
				fmt.Fprintf(os.Stderr, "Error: Path for -f (%s) is a directory, must be a file.\n", followUpFilePath)
			} else {
				var displayFollowUpPath string
				if filepath.IsAbs(followUpFilePath) {
					displayFollowUpPath = filepath.Clean(followUpFilePath)
				} else {
					displayFollowUpPath = followUpFilePath
				}

				if outputBuilder.Len() > 0 {
					outputBuilder.WriteString("\n\n")
				}
				processFile(absFollowUpPath, displayFollowUpPath, &outputBuilder)
			}
		}
	}

	finalOutput := outputBuilder.String()

	if strings.TrimSpace(finalOutput) == "" {
		fmt.Fprintln(os.Stderr, "Warning: Output is empty or contains only whitespace.")
	} else {
		// Calculate and print the elaborated token estimate
		_, details := estimateTokens(finalOutput)
		fmt.Fprintf(os.Stderr, "Estimated token count: %s\n", details)
	}

	// Output handling
	if *stdoutPtr {
		fmt.Print(finalOutput)
		fmt.Fprintln(os.Stderr, "Content written to stdout.")
	} else if *outputFilePtr != "" {
		filePath := *outputFilePtr
		err := os.WriteFile(filePath, []byte(finalOutput), 0644)
		if err != nil {
			log.Fatalf("Failed to write to output file %s: %v", filePath, err)
		}
		fmt.Fprintf(os.Stderr, "Content written to file: %s\n", filePath)
	} else {
		// Clipboard output (default)
		copyToClipboard(finalOutput, *termCopyPtr)
	}
}

// copyToClipboard handles the logic of copying text to the system clipboard
// using various methods, prioritizing terminal-friendly ones if requested.
func copyToClipboard(content string, useTermAware bool) {
	if strings.TrimSpace(content) == "" {
		fmt.Fprintln(os.Stderr, "No content to copy to clipboard.")
		return
	}

	// Terminal-Aware Copy (OSC 52)
	// This is the best method for remote sessions with compatible terminals (Kitty, etc.)
	if useTermAware {
		term := os.Getenv("TERM")
		// Check for common terminal types that support OSC 52
		if strings.Contains(term, "kitty") || strings.Contains(term, "xterm") || os.Getenv("TMUX") != "" {
			fmt.Fprintln(os.Stderr, "Attempting clipboard copy via OSC 52 escape code...")
			encodedContent := base64.StdEncoding.EncodeToString([]byte(content))
			// OSC 52 format: \x1b]52;c;<base64-data>\x07
			// The 'c' refers to the clipboard selection.
			// Wrap in DCS for tmux compatibility: \x1bPtmux;\x1b\x1b]52;...
			if os.Getenv("TMUX") != "" {
				fmt.Printf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", encodedContent)
			} else {
				fmt.Printf("\x1b]52;c;%s\x07", encodedContent)
			}
			fmt.Fprintln(os.Stderr, "Content sent to terminal for clipboard (OSC 52).")
			return
		}
	}

	// Kitty Kitten Clipboard (Very reliable if inside Kitty)
	// This is a great fallback if OSC 52 is disabled for some reason.
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		// First, check if the 'kitty' command is available in the PATH
		kittyPath, err := exec.LookPath("kitty")
		if err == nil {
			fmt.Fprintln(os.Stderr, "Attempting clipboard copy via `kitty +kitten clipboard`...")
			cmd := exec.Command(kittyPath, "+kitten", "clipboard")
			cmd.Stdin = strings.NewReader(content)
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				fmt.Fprintln(os.Stderr, "Content copied to clipboard via `kitty +kitten clipboard`.")
				return
			}
		}
	}

	// External Command-Line Tools
	// Search for common clipboard utilities.
	tools := []string{"wl-copy", "xclip -selection clipboard", "xsel --clipboard"}
	for _, tool := range tools {
		parts := strings.Fields(tool)
		path, err := exec.LookPath(parts[0])
		if err != nil {
			continue
		}

		fmt.Fprintf(os.Stderr, "Attempting clipboard copy via `%s`...\n", tool)
		cmd := exec.Command(path, parts[1:]...)
		cmd.Stdin = strings.NewReader(content)
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err == nil {
			fmt.Fprintf(os.Stderr, "Content copied to clipboard via `%s`.\n", tool)
			return
		} else {
			fmt.Fprintf(os.Stderr, "Failed to copy with `%s`: %v\n", tool, err)
		}
	}

	// Fallback to Go Library
	// This often fails over SSH but is a good last resort for local desktop sessions.
	fmt.Fprintln(os.Stderr, "Falling back to default clipboard library (may not work over SSH)...")
	if err := clipboard.Init(); err != nil {
		log.Fatalf("Failed to initialize clipboard library: %v\nPlease install xclip/xsel or wl-clipboard, or use -t.", err)
	}
	clipboard.Write(clipboard.FmtText, []byte(content))
	fmt.Fprintln(os.Stderr, "Content copied to clipboard!")
}

// processDirectory walks a directory and processes all files within it.
func processDirectory(absDirPath string, baseDisplayPath string, builder *strings.Builder, excludePatterns []string) {
	fmt.Fprintf(os.Stderr, "Processing directory: %s\n", baseDisplayPath)
	filepath.WalkDir(absDirPath, func(currentAbsPath string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", currentAbsPath, errWalk)
			if d == nil {
				return errWalk
			}
			return nil
		}

		// Don't process the root directory entry itself, just continue the walk.
		if currentAbsPath == absDirPath {
			return nil
		}

		// Calculate relative path for all subsequent checks
		relativePath, err := filepath.Rel(absDirPath, currentAbsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calculating relative path: %v. Skipping.\n", err)
			return nil
		}

		// Check against user-defined exclude patterns
		if excluded, pattern := isExcluded(relativePath, excludePatterns); excluded {
			// Don't log exclusion of .git folder as it is very common
			if d.Name() != ".git" {
				fmt.Fprintf(os.Stderr, "Skipping excluded path: %s (pattern: '%s')\n", relativePath, pattern)
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle directories (check for hidden ones)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				// Don't log exclusion of .git folder
				if d.Name() != ".git" {
					fmt.Fprintf(os.Stderr, "Skipping hidden directory: %s\n", relativePath)
				}
				return filepath.SkipDir
			}
			return nil
		}

		// Handle files (we are guaranteed it's a file at this point)
		if strings.HasPrefix(d.Name(), ".") {
			fmt.Fprintf(os.Stderr, "Skipping hidden file: %s\n", relativePath)
			return nil
		}

		// Process the file
		displayFilePath := filepath.ToSlash(filepath.Join(baseDisplayPath, relativePath))
		processFile(currentAbsPath, displayFilePath, builder)
		return nil
	})
}

// processFile reads a file and appends its content formatted as a markdown code block to the builder.
func processFile(absFilePath string, displayFilePath string, builder *strings.Builder) {
	content, err := os.ReadFile(absFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", displayFilePath, err)
		return
	}

	if len(content) > 1*1024*1024 {
		fmt.Fprintf(os.Stderr, "Skipping large file (> 1MB): %s\n", displayFilePath)
		return
	}

	isBinary := false
	for i, b := range content {
		if b == 0 {
			if i < 10 && (len(content) > i+1 && content[i+1] == 0) {
				continue
			}
			isBinary = true
			break
		}
	}
	if isBinary {
		fmt.Fprintf(os.Stderr, "Skipping likely binary file: %s\n", displayFilePath)
		return
	}

	fmt.Fprintf(os.Stderr, "Adding file: %s\n", displayFilePath)

	if builder.Len() > 0 {
		builder.WriteString("\n\n")
	}

	lang := getLanguageHint(absFilePath)
	header := displayFilePath
	if lang != "" {
		header = lang + " " + displayFilePath
	}

	builder.WriteString(fmt.Sprintf("```%s\n", header))
	builder.Write(content)
	// Ensure content ends with a newline before closing backticks if it doesn't already
	if len(content) > 0 && content[len(content)-1] != '\n' {
		builder.WriteByte('\n')
	}
	builder.WriteString("```\n")
}

// getLanguageHint determines a language hint from the file extension.
func getLanguageHint(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath))

	switch baseName {
	case "caddyfile":
		return "caddyfile"
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile":
		return "makefile"
	}

	switch ext {
	case ".go":
		return "go"
	case ".md", ".markdown":
		return "markdown"
	case ".sh", ".bash":
		return "bash"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cxx", ".hpp", ".hxx", ".cc", ".hh":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".rs":
		return "rust"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".xml":
		return "xml"
	case ".sql":
		return "sql"
	case ".dockerfile":
		return "dockerfile"
	case ".txt", ".text":
		return "text"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
