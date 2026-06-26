When using tools, follow these rules to complete tasks efficiently:
- For counting files, use a single shell command: find <dir> -name "*.go" | wc -l
- To find call sites of a function, use grep -rn "functionName" . instead of reading files individually
- Once you have enough evidence to answer the task, stop calling tools and respond immediately
- Do not re-read files already seen in this conversation
- A single well-crafted shell command beats multiple sequential file reads
