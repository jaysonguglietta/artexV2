/* Shared catalog-key validation. Compare flattened target keys with the English baseline and report both missing and extra entries. CLI and Vitest use the same pure logic. */

export interface KeyDiff {
  missing: string[]; // Present in baseline but missing from target.
  extra: string[]; // Present in target but absent from baseline.
}

/* Flatten nested objects to dotted keys; arrays/primitives are leaves. */
export function flattenKeys(tree: unknown, prefix = ""): Set<string> {
  const out = new Set<string>();
  if (tree === null || typeof tree !== "object" || Array.isArray(tree)) {
    if (prefix) out.add(prefix);
    return out;
  }
  for (const [key, value] of Object.entries(tree as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${key}` : key;
    for (const leaf of flattenKeys(value, path)) out.add(leaf);
  }
  return out;
}

/* Return sorted missing/extra keys relative to baseline. */
export function diffKeys(baseline: Set<string>, target: Set<string>): KeyDiff {
  const missing = [...baseline].filter((k) => !target.has(k)).sort();
  const extra = [...target].filter((k) => !baseline.has(k)).sort();
  return { missing, extra };
}

/* True when neither missing nor extra keys remain. */
export function isInSync(diff: KeyDiff): boolean {
  return diff.missing.length === 0 && diff.extra.length === 0;
}
