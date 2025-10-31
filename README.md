# `fcopy` - Effortless File-to-Clipboard for AI Prompts

`fcopy` markdownize a list of files or directory to be used in LLM prompting.



## Installation
  ```
  go install github.com/akhenakh/fcopy@latest
  ```

Or on Windows, add the directory containing `fcopy.exe` to your system's PATH environment variable.


## Supercharge with `fzf` (Interactive File Selection)

For an even smoother workflow, you can combine `fcopy` with `fzf` (a command-line fuzzy finder) to interactively select files and directories.

Once `fzf` is integrated with your shell, you can often use `**<Tab>` to trigger `fzf` for file selection:

```bash
# Type fcopy, then space, then ** and press Tab
fcopy **<Tab>
```
This will open an `fzf` interface allowing you to select one or more files/directories. After selection, their names will be inserted into the command line.

## Use `-p` to pass your prompt

`fcopy -p "refactor that file" main.go`

## Using `-f` for Reusable Prompt Templates/Rules

The `-f` flag is used to pass "rule files" or "template files" that contain standard instructions you want to give to an LLM.

**Example:** You want the LLM to always render its output in a specific markdown format.

Create a file, say `~/ai_rules/always_markdown.md`:
```markdown
Please analyze the provided content.
Your entire response MUST be formatted as valid Markdown.
Use headings for major sections and bullet points for lists.
If code examples are necessary, ensure they are in fenced code blocks with appropriate language tags.
```
Now, when you want to copy some code and apply these rules:
```bash
fcopy my_script.py -f ~/ai_rules/always_markdown.md
```
The content of `my_script.py` will be copied, followed by the instructions from `always_markdown.md`. This ensures consistency and saves you from retyping common directives.

## Using -x to Exclude Files and Directories

The -x flag allows you to specify a comma-separated list of glob patterns to exclude from the output. This is useful for ignoring build artifacts, version control directories, logs, or any other files you don't want to include in the context.

Common Patterns:

    By name: -x .git,node_modules,dist

    By extension: -x "*.log,*.tmp"

    By path: -x "internal/testdata/*,docs/images"

Example: Copy the entire project directory but exclude the .git folder, all markdown files, and the build directory.
code Bash

    
```bash
fcopy -x ".git,*.md,build" .
```

  


## Why `fcopy`?

When working with AI, you often need to provide code, configuration files, or entire directory structures as context. Manually opening, copying, and formatting this content is tedious and error-prone. `fcopy` automates this by:

*   Processing multiple files and directories.
*   Formatting content into markdown code blocks with language hints.
*   Displaying clear, relative paths for each file.
*   Allowing you to append a custom prompt (`-p`).
*   Letting you append content from another file (`-f`), perfect for reusable instructions or context.
*   Skipping hidden files/directories, binary files, and overly large files.
*   Putting the result in your clipboard
