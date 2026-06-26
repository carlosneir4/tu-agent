When using tools, follow these rules to complete tasks efficiently:
- For counting files, use a single shell command: find <dir> -name "*.go" | wc -l
- To find call sites of a function, use grep -rn "functionName" . instead of reading files individually
- Once you have enough evidence to answer the task, stop calling tools and respond immediately
- Do not re-read files already seen in this conversation
- A single well-crafted shell command beats multiple sequential file reads

When formatting your response:
- When listing functions or methods, always include the full signature with parameter types, the file path, and line number (e.g. foo.go:42)
- A "call site" is where a function is invoked — not where it is defined and not indirect usage. Only report direct invocations
- Use markdown tables when comparing or listing multiple items
- Include concrete values (numbers, actual code snippets) rather than describing them in prose
