import { readFileSync, existsSync } from "fs";
import { join } from "path";
import { syncFromLocalThemisto } from "../src/server/sync";

function loadEnv(root: string) {
  for (const file of [".env.local", ".env"]) {
    const fullPath = join(root, file);
    if (!existsSync(fullPath)) continue;
    const content = readFileSync(fullPath, "utf8");
    for (const line of content.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#") || !trimmed.includes("=")) continue;
      const [key, ...rest] = trimmed.split("=");
      if (!process.env[key]) process.env[key] = rest.join("=").trim();
    }
  }
}

async function main() {
  loadEnv(process.cwd());
  const result = await syncFromLocalThemisto();
  console.log(`Imported ${result.importedOrganizations} organizations and ${result.importedDevices} devices.`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
