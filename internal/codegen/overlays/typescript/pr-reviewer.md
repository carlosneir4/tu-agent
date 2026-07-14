## TypeScript-specific review checks

- **Type safety**: `any` types without justification? Type assertions (`as`) narrowing correctly?
- **Null safety**: optional chaining (`?.`) and nullish coalescing (`??`) where needed?
- **Async safety**: all Promises awaited or explicitly handled? Unhandled rejections?
- **Bundle impact**: new imports from large libraries using tree-shakeable imports?
- **Module system**: imports consistent with project's ESM or CJS configuration?
- **strict mode**: is `tsconfig` `strict: true` maintained? Any new `@ts-ignore` without comment?
