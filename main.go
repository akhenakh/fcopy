package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
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

	err := clipboard.Init()
	if err != nil {
		log.Fatalf("Failed to initialize clipboard: %v", err)
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
				// Determine display path for the follow-up file
				var displayFollowUpPath string
				if filepath.IsAbs(followUpFilePath) {
					displayFollowUpPath = filepath.Clean(followUpFilePath)
				} else {
					displayFollowUpPath = followUpFilePath
				}

				if outputBuilder.Len() > 0 {
					outputBuilder.WriteString("\n\n") // Separator
				}
				// Use processFile to read, format, and append
				processFile(absFollowUpPath, displayFollowUpPath, &outputBuilder)
				// processFile already prints "Adding file: ..." or errors to stderr
			}
		}
	}

	finalOutput := outputBuilder.String()
	if strings.TrimSpace(finalOutput) == "" { // Check if effectively empty after trimming whitespace
		fmt.Println("No content processed or provided.")
		return
	}

	clipboard.Write(clipboard.FmtText, []byte(finalOutput))
	fmt.Println("Content copied to clipboard!")
	// For debugging, uncomment below:
	// fmt.Println("\n---BEGIN CLIPBOARD CONTENT---")
	// fmt.Println(finalOutput)
	// fmt.Println("---END CLIPBOARD CONTENT---")
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
			fmt.Fprintf(os.Stderr, "Error calculating relative path for %s (base %s): %v\n", currentAbsPath, absDirPath, err)
			processFile(currentAbsPath, currentAbsPath, builder)
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
	isBinary := false
	for i, b := range content {
		if b == 0 {
			// Allow a few null bytes at the beginning for UTF-16 BOM, etc.
			// but if they appear later or frequently, it's likely binary.
			// This is a heuristic.
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
	builder.WriteString("\n```\n\n")
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
	case ".dockerfile": // Extension might also be used
		return "dockerfile"
	case ".txt", ".text":
		return "text" // Or "" for auto-detect by highlighter
	case "":
		return "" // No hint if no extension and not a known filename
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
