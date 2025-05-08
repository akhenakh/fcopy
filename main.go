package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec" // Added for command execution
	"path/filepath"
	"strings"

	"golang.design/x/clipboard"
)

func main() {
	// Define flags
	promptPtr := flag.String("p", "", "A prompt to append after the main file contents")
	followUpFilePtr := flag.String("f", "", "Path to a file whose content will be appended after the prompt, formatted as markdown")
	outputFilePtr := flag.String("o", "", "Output to the specified file instead of clipboard")
	stdoutPtr := flag.Bool("s", false, "Output to stdout instead of clipboard")

	// Custom usage message
	flag.Usage = func() {
		progName := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <path1> [path2 ...]\n", progName)
		fmt.Fprintf(os.Stderr, "Processes files/directories, formats them as markdown, optionally appends a prompt and/or content from another file.\n")
		fmt.Fprintf(os.Stderr, "Output is sent to clipboard by default, or to a file with -o, or to stdout with -s.\n")
		fmt.Fprintf(os.Stderr, "\nArguments (Processed First):\n")
		fmt.Fprintf(os.Stderr, "  <path1> [path2 ...]  Paths to files or directories to process.\n")
		fmt.Fprintf(os.Stderr, "\nOptions (Appended in Order):\n")
		flag.PrintDefaults() // Prints help for all defined flags
		fmt.Fprintf(os.Stderr, "\nOutput Destination (Mutually Exclusive):\n")
		fmt.Fprintf(os.Stderr, "  -o <filepath>      Output to the specified file.\n")
		fmt.Fprintf(os.Stderr, "  -s                 Output to stdout.\n")
		fmt.Fprintf(os.Stderr, "  (default)          Output to clipboard.\n")
		fmt.Fprintf(os.Stderr, "  Note: -o and -s are mutually exclusive. If neither is specified, output goes to clipboard.\n")
		fmt.Fprintf(os.Stderr, "\nOrder of Output Content:\n")
		fmt.Fprintf(os.Stderr, "  1. Content from <path1>, <path2>, ...\n")
		fmt.Fprintf(os.Stderr, "  2. Text from -p \"prompt\"\n")
		fmt.Fprintf(os.Stderr, "  3. Formatted content from -f <filepath>\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s internal/ README.md\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Review this code\" main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Context:\" main.go -f context_details.txt\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -f instructions.md\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -o output.md main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -s internal/ | less\n", progName)
	}

	flag.Parse() // Parse the command-line flags

	// Check for mutually exclusive output options
	if *stdoutPtr && *outputFilePtr != "" {
		fmt.Fprintf(os.Stderr, "Error: -s (stdout) and -o (output file) options are mutually exclusive.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	paths := flag.Args() // Get the non-flag arguments (paths)

	if len(paths) == 0 && *promptPtr == "" && *followUpFilePtr == "" {
		flag.Usage()
		os.Exit(1)
	}

	var outputBuilder strings.Builder

	// 1. Process positional path arguments
	for _, argPath := range paths {
		absPath, err := filepath.Abs(argPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for %s: %v\n", argPath, err)
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Path does not exist: %s\n", argPath)
			} else {
				fmt.Fprintf(os.Stderr, "Error stating path %s: %v\n", argPath, err)
			}
			continue
		}

		var displayPathBase string
		if filepath.IsAbs(argPath) {
			displayPathBase = filepath.Clean(argPath)
		} else {
			displayPathBase = argPath
		}

		if info.IsDir() {
			processDirectory(absPath, displayPathBase, &outputBuilder)
		} else {
			processFile(absPath, displayPathBase, &outputBuilder)
		}
	}

	// 2. Append the prompt from -p if provided
	promptText := *promptPtr
	if promptText != "" {
		if outputBuilder.Len() > 0 {
			outputBuilder.WriteString("\n\n") // Two newlines for good separation
		}
		outputBuilder.WriteString(promptText)
		fmt.Fprintf(os.Stderr, "Appended prompt text.\n")
	}

	// 3. Append content from the -f file if provided
	followUpFilePath := *followUpFilePtr
	if followUpFilePath != "" {
		absFollowUpPath, err := filepath.Abs(followUpFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for follow-up file -f %s: %v\n", followUpFilePath, err)
		} else {
			info, err := os.Stat(absFollowUpPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "Follow-up file -f %s does not exist.\n", followUpFilePath)
				} else {
					fmt.Fprintf(os.Stderr, "Error stating follow-up file -f %s: %v\n", followUpFilePath, err)
				}
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
					outputBuilder.WriteString("\n\n") // Separator
				}
				processFile(absFollowUpPath, displayFollowUpPath, &outputBuilder)
			}
		}
	}

	finalOutput := outputBuilder.String()

	if strings.TrimSpace(finalOutput) == "" {
		fmt.Fprintln(os.Stderr, "Warning: Output is empty or contains only whitespace.")
		// For clipboard, we will explicitly skip. For file/stdout, empty output will be written.
	}

	// Output handling
	if *stdoutPtr {
		fmt.Print(finalOutput) // Print to stdout
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
		if strings.TrimSpace(finalOutput) == "" {
			fmt.Fprintln(os.Stderr, "No content to copy to clipboard.")
			return
		}

		if os.Getenv("WAYLAND_DISPLAY") != "" {
			wlCopyPath, err := exec.LookPath("wl-copy")
			if err != nil {
				log.Printf("Wayland: 'wl-copy' command not found in PATH. Please ensure 'wl-clipboard' is installed.")
				log.Printf("Falling back to default clipboard library method. Note potential 64k limitation.")
				if errInit := clipboard.Init(); errInit != nil {
					log.Fatalf("Fallback: Failed to initialize clipboard: %v", errInit)
				}
				clipboard.Write(clipboard.FmtText, []byte(finalOutput))
				fmt.Fprintln(os.Stderr, "Content copied to clipboard using default library (wl-copy not found).")
			} else {
				tempFile, err := os.CreateTemp("", "fcopy-clipboard-*.txt")
				if err != nil {
					log.Fatalf("Wayland: Failed to create temp file: %v", err)
				}
				tempFilePath := tempFile.Name()
				defer os.Remove(tempFilePath)

				_, errWrite := tempFile.WriteString(finalOutput)
				errClose := tempFile.Close()

				if errWrite != nil {
					log.Fatalf("Wayland: Failed to write to temp file %s: %v", tempFilePath, errWrite)
				}
				if errClose != nil {
					log.Fatalf("Wayland: Failed to close temp file %s after writing: %v", tempFilePath, errClose)
				}

				fileForStdin, err := os.Open(tempFilePath)
				if err != nil {
					log.Fatalf("Wayland: Failed to open temp file %s for wl-copy stdin: %v", tempFilePath, err)
				}
				defer fileForStdin.Close()

				cmd := exec.Command(wlCopyPath)
				cmd.Stdin = fileForStdin
				cmd.Stderr = os.Stderr

				if err := cmd.Run(); err != nil {
					log.Fatalf("Wayland: wl-copy command failed (command: %s, tempfile: %s): %v", wlCopyPath, tempFilePath, err)
				}
				fmt.Fprintln(os.Stderr, "Content copied to clipboard via wl-copy!")
			}
		} else {
			if err := clipboard.Init(); err != nil {
				log.Fatalf("Failed to initialize clipboard: %v", err)
			}
			clipboard.Write(clipboard.FmtText, []byte(finalOutput))
			fmt.Fprintln(os.Stderr, "Content copied to clipboard!")
		}
	}
}

// processDirectory walks a directory and processes all files within it.
func processDirectory(absDirPath string, baseDisplayPath string, builder *strings.Builder) {
	fmt.Fprintf(os.Stderr, "Processing directory: %s\n", baseDisplayPath)
	filepath.WalkDir(absDirPath, func(currentAbsPath string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", currentAbsPath, errWalk)
			if d == nil {
				return errWalk
			}
			return nil
		}

		if d.IsDir() {
			if currentAbsPath == absDirPath {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				fmt.Fprintf(os.Stderr, "Skipping hidden directory: %s\n", currentAbsPath)
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			fmt.Fprintf(os.Stderr, "Skipping hidden file: %s\n", currentAbsPath)
			return nil
		}

		relativePathInDir, err := filepath.Rel(absDirPath, currentAbsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calculating relative path for %s (base %s): %v. Using absolute path for display.\n", currentAbsPath, absDirPath, err)
			processFile(currentAbsPath, currentAbsPath, builder)
			return nil
		}

		displayFilePath := filepath.ToSlash(filepath.Join(baseDisplayPath, relativePathInDir))
		processFile(currentAbsPath, displayFilePath, builder)
		return nil
	})
}

// processFile reads a file and appends its content formatted as a markdown code block to the builder.
func processFile(absFilePath string, displayFilePath string, builder *strings.Builder) {
	content, err := os.ReadFile(absFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s (displaying as %s): %v\n", absFilePath, displayFilePath, err)
		return
	}

	if len(content) > 1*1024*1024 { // 1MB limit
		fmt.Fprintf(os.Stderr, "Skipping large file ( > 1MB): %s\n", displayFilePath)
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
		fmt.Fprintf(os.Stderr, "Skipping likely binary file (contains null bytes): %s\n", displayFilePath)
		return
	}

	fmt.Fprintf(os.Stderr, "Adding file: %s\n", displayFilePath)

	if builder.Len() > 0 { // Add a separator if there's existing content
		builder.WriteString("\n\n") // Ensures a blank line before the new code block
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
	builder.WriteString("```\n") // Single newline after the closing backticks
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
	case "":
		return ""
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
