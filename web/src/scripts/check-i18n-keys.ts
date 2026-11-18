/** Check that every supported locale has exactly the English catalog's keys. */

import { diffKeys, flattenKeys, isInSync } from "./i18n-keys";
import fs from "node:fs";
import path from "node:path";

const MESSAGES_DIR = path.resolve(__dirname, "../../messages");
const BASELINE = "en";
const LOCALES = ["en"] as const;

function readLocale(locale: string): Set<string> {
  const file = path.join(MESSAGES_DIR, `${locale}.json`);
  const tree = JSON.parse(fs.readFileSync(file, "utf8")) as unknown;
  return flattenKeys(tree);
}

function preview(keys: string[]): string {
  const shown = keys.slice(0, 20).join(", ");
  return keys.length > 20 ? `${shown} …` : shown;
}

const baseline = readLocale(BASELINE);
let failed = false;

console.log("─".repeat(64));
console.log(`i18n key consistency (baseline: ${BASELINE}.json, ${baseline.size} keys)`);
console.log("─".repeat(64));

for (const locale of LOCALES) {
  const diff = diffKeys(baseline, readLocale(locale));
  if (isInSync(diff)) {
    console.log(`[OK] ${locale}.json — matches`);
    continue;
  }
  failed = true;
  console.log(`[FAIL] ${locale}.json — differs`);
  if (diff.missing.length) console.log(`   Missing ${diff.missing.length}: ${preview(diff.missing)}`);
  if (diff.extra.length) console.log(`   Extra ${diff.extra.length}: ${preview(diff.extra)}`);
}

console.log("─".repeat(64));
if (failed) {
  console.error("Catalog keys differ. Reconcile missing/extra keys and rerun the check.");
  process.exit(1);
}
console.log("All locale key sets match.");
