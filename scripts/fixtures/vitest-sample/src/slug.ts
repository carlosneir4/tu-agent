/** Small text helpers — fixture for the testgen criterion run. */

export function slugify(text: string): string {
  const words = text.trim().toLowerCase().split(/\s+/).filter((w) => w.length > 0);
  return words.join("-");
}

export function truncate(text: string, limit: number): string {
  if (limit <= 0) return "";
  if (text.length <= limit) return text;
  return text.slice(0, limit - 1) + "…";
}

export function wordCount(text: string): number {
  return text.split(/\s+/).filter((w) => w.length > 0).length;
}
