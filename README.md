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

## Usage

### Basic Usage

Copy a file and a directory:
```bash
fcopy main.go internal/
```

### Add a Prompt (`-p`)

Pass a prompt to be appended to the output:

```bash
fcopy -p "Refactor this code to use the new API" main.go
```

### Append Rule Files (`-f`)

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

### Process a Git Repository (`-g`)

You can directly process a remote Git repository. `fcopy` will perform a shallow clone to a temporary directory, process the files, and then clean up.

```bash
fcopy -g https://github.com/user/repo
```

### Excluding Files (`-x` and `.gitignore`)

**Using the Flag:**
The `-x` flag allows you to specify a comma-separated list of glob patterns to exclude from the output.

```bash
fcopy -x ".git,*.md,build" .
```

**Using .gitignore:**
If `fcopy` detects a `.gitignore` file in the root of the directory being processed (or the root of a cloned git repo), it will automatically parse it and exclude the listed patterns.

*Note: This implementation supports standard glob patterns found in gitignore (like `*.log`, `node_modules/`, `dist`) but implies basic matching. Deeply nested negation patterns or complex wildcards may vary slightly from native git behavior.*

## Why `fcopy`?

When working with AI, you often need to provide code, configuration files, or entire directory structures as context. Manually opening, copying, and formatting this content is tedious and error-prone. `fcopy` automates this by:

*   Processing multiple files and directories.
*   Formatting content into markdown code blocks with language hints.
*   **Git Aware:** Automatically respects `.gitignore` files.
*   **Remote Repos:** Can clone and process a repository URL in one command (`-g`).
*   Displaying clear, relative paths for each file.
*   Allowing you to append a custom prompt (`-p`).
*   Letting you append content from another file (`-f`), perfect for reusable instructions or context.
*   Skipping hidden files/directories, binary files, and overly large files.
*   Putting the result in your clipboard
