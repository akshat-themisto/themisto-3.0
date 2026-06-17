import { readFileSync, existsSync } from "fs";
import { join } from "path";

let loaded = false;

export function loadLocalEnv(root = process.cwd()) {
  if (loaded) return;
  const candidates = [".env.local", ".env"];
  for (const file of candidates) {
    const fullPath = join(root, file);
    if (!existsSync(fullPath)) continue;
    const content = readFileSync(fullPath, "utf8");
    for (const line of content.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#") || !trimmed.includes("=")) continue;
      const [key, ...rest] = trimmed.split("=");
      if (!process.env[key]) {
        process.env[key] = rest.join("=").trim();
      }
    }
  }
  loaded = true;
}

