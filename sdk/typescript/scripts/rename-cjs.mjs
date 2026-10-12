// tsc emits CommonJS as .js; the package is "type": "module", so rename to .cjs.
import { readdirSync, renameSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const dir = "dist/cjs";
for (const f of readdirSync(dir)) {
  if (f.endsWith(".js")) renameSync(join(dir, f), join(dir, f.replace(/\.js$/, ".cjs")));
  if (f.endsWith(".d.ts")) renameSync(join(dir, f), join(dir, f.replace(/\.d\.ts$/, ".d.cts")));
  if (f.endsWith(".js.map")) {
    const to = f.replace(/\.js\.map$/, ".cjs.map");
    renameSync(join(dir, f), join(dir, to));
  }
}
for (const f of readdirSync(dir)) {
  if (f.endsWith(".cjs")) {
    const p = join(dir, f);
    writeFileSync(p, readFileSync(p, "utf8").replace(/\/\/# sourceMappingURL=.*\.js\.map/, (m) => m.replace(".js.map", ".cjs.map")));
  }
}
