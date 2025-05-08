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

	// Custom usage message
	flag.Usage = func() {
		progName := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <path1> [path2 ...]\n", progName)
		fmt.Fprintf(os.Stderr, "Processes files/directories, formats them as markdown, optionally appends a prompt and/or content from another file, and copies to clipboard.\n")
		fmt.Fprintf(os.Stderr, "\nArguments (Processed First):\n")
		fmt.Fprintf(os.Stderr, "  <path1> [path2 ...]  Paths to files or directories to process.\n")
		fmt.Fprintf(os.Stderr, "\nOptions (Appended in Order):\n")
		flag.PrintDefaults() // Prints help for all defined flags
		fmt.Fprintf(os.Stderr, "\nOrder of Output:\n")
		fmt.Fprintf(os.Stderr, "  1. Content from <path1>, <path2>, ...\n")
		fmt.Fprintf(os.Stderr, "  2. Text from -p \"prompt\"\n")
		fmt.Fprintf(os.Stderr, "  3. Formatted content from -f <filepath>\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s internal/ README.md\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Review this code\" main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Context:\" main.go -f context_details.txt\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -f instructions.md\n", progName)
	}

	flag.Parse() // Parse the command-line flags

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
		fmt.Println("No content processed or provided.")
		return
	}

	// Clipboard handling for wayland (piping would be limited to 64k)
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		wlCopyPath, err := exec.LookPath("wl-copy")
		if err != nil {
			log.Printf("Wayland: 'wl-copy' command not found in PATH. Please ensure 'wl-clipboard' is installed.")
			log.Printf("Falling back to default clipboard library method. 64k limitation")
			// Fallback to default library if wl-copy is not found
			if errInit := clipboard.Init(); errInit != nil {
				log.Fatalf("Fallback: Failed to initialize clipboard: %v", errInit)
			}
			clipboard.Write(clipboard.FmtText, []byte(finalOutput))
			fmt.Println("Content copied to clipboard using default library (wl-copy not found).")
		} else {
			// wl-copy found, proceed with temp file method
			tempFile, err := os.CreateTemp("", "fcopy-clipboard-*.txt")
			if err != nil {
				log.Fatalf("Wayland: Failed to create temp file: %v", err)
			}
			tempFilePath := tempFile.Name()
			// Defer removal of the temp file. This runs when main() exits,
			// unless log.Fatalf causes an earlier exit.
			defer os.Remove(tempFilePath)

			_, errWrite := tempFile.WriteString(finalOutput)
			// Always close the file descriptor obtained from os.CreateTemp
			errClose := tempFile.Close()

			if errWrite != nil {
				log.Fatalf("Wayland: Failed to write to temp file %s: %v", tempFilePath, errWrite)
			}
			if errClose != nil {
				log.Fatalf("Wayland: Failed to close temp file %s after writing: %v", tempFilePath, errClose)
			}

			// Open the temp file for reading to pass to wl-copy's stdin
			fileForStdin, err := os.Open(tempFilePath)
			if err != nil {
				log.Fatalf("Wayland: Failed to open temp file %s for wl-copy stdin: %v", tempFilePath, err)
			}
			defer fileForStdin.Close() // Close the reader for stdin

			cmd := exec.Command(wlCopyPath)
			cmd.Stdin = fileForStdin
			cmd.Stderr = os.Stderr // Pipe wl-copy's errors to our stderr for visibility

			if err := cmd.Run(); err != nil {
				log.Fatalf("Wayland: wl-copy command failed (command: %s, tempfile: %s): %v", wlCopyPath, tempFilePath, err)
			}
			fmt.Println("Content copied to clipboard via wl-copy!")
		}
	} else {
		// Not Wayland, or fallback was not taken above, use the standard clipboard library
		err := clipboard.Init()
		if err != nil {
			log.Fatalf("Failed to initialize clipboard: %v", err)
		}
		clipboard.Write(clipboard.FmtText, []byte(finalOutput))
		fmt.Println("Content copied to clipboard!")
	}
}

// processDirectory walks a directory and processes all files within it.
// absDirPath: absolute path to the directory to walk.
// baseDisplayPath: the path argument as provided by the user (e.g., "internal/", "narun/caddynarun").
// This is used to construct user-friendly display paths for files within the directory.
func processDirectory(absDirPath string, baseDisplayPath string, builder *strings.Builder) {
	fmt.Fprintf(os.Stderr, "Processing directory: %s\n", baseDisplayPath)
	filepath.WalkDir(absDirPath, func(currentAbsPath string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", currentAbsPath, errWalk)
			if d == nil { // If d is nil, the error is likely fatal for this path.
				return errWalk
			}
			return nil // Otherwise, attempt to continue with other files/dirs.
		}

		if d.IsDir() {
			if currentAbsPath == absDirPath { // Don't skip the root directory itself initially
				return nil
			}
			// Skip hidden directories (e.g., .git, .vscode)
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				fmt.Fprintf(os.Stderr, "Skipping hidden directory: %s\n", currentAbsPath)
				return filepath.SkipDir
			}
			return nil // Continue walking
		}

		// Skip hidden files (e.g., .DS_Store)
		if strings.HasPrefix(d.Name(), ".") {
			fmt.Fprintf(os.Stderr, "Skipping hidden file: %s\n", currentAbsPath)
			return nil
		}

		// Construct the display path relative to the initially provided baseDisplayPath
		relativePathInDir, err := filepath.Rel(absDirPath, currentAbsPath)
		if err != nil {
			// Fallback to absolute path if Rel fails (should be rare)
			fmt.Fprintf(os.Stderr, "Error calculating relative path for %s (base %s): %v. Using absolute path for display.\n", currentAbsPath, absDirPath, err)
			processFile(currentAbsPath, currentAbsPath, builder) // Use absolute path as display path
			return nil
		}

		displayFilePath := filepath.ToSlash(filepath.Join(baseDisplayPath, relativePathInDir))
		processFile(currentAbsPath, displayFilePath, builder)
		return nil
	})
}

// processFile reads a file and appends its content formatted as a markdown code block to the builder.
// absFilePath: absolute path to the file to read.
// displayFilePath: the path to display in the markdown header (e.g., "internal/service.go").
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

	// Basic binary detection (null bytes)
	isBinary := false
	for i, b := range content {
		if b == 0 {
			// Allow a few null bytes at the beginning for UTF-16 BOM, etc.
			// but if they appear later or frequently, it's likely binary.
			if i < 10 && (len(content) > i+1 && content[i+1] == 0) { // simple check for wide chars
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

	lang := getLanguageHint(absFilePath)
	header := displayFilePath
	if lang != "" {
		header = lang + " " + displayFilePath
	}

	builder.WriteString(fmt.Sprintf("```%s\n", header))
	builder.Write(content)
	builder.WriteString("\n```\n\n") // Ensure two newlines after the code block for separation
}

// getLanguageHint determines a language hint from the file extension.
func getLanguageHint(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath)) // For filename specific hints

	// Check for specific filenames first
	switch baseName {
	case "caddyfile":
		return "caddyfile"
	case "dockerfile", "containerfile": // containerfile is another common name
		return "dockerfile"
	case "makefile":
		return "makefile"
	}

	// Then check extensions
	switch ext {
	case ".go":
		return "go"
	case ".md", ".markdown":
		return "markdown"
	case ".sh", ".bash": // .bash common for bash-specific scripts
		return "bash"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs": // .mjs for ES Modules, .cjs for CommonJS
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cxx", ".hpp", ".hxx", ".cc", ".hh": // More C++ extensions
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts": // .kts for Kotlin Script
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
	case ".dockerfile": // Extension might also be used, though basename is more common
		return "dockerfile"
	case ".txt", ".text":
		return "text" // Or "" for auto-detect by highlighter
	case "": // No extension
		return "" // No hint if no extension and not a known filename
	default:
		// For unknown extensions, provide the extension itself as a hint (without the dot)
		return strings.TrimPrefix(ext, ".")
	}
}
