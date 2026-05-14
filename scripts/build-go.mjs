import { spawnSync } from "node:child_process";
import { spiderFingerprint } from "./spider-fingerprint.mjs";

const out = process.env.GO_BUILD_OUT || "openlist-tvbox.exe";
const fingerprint = spiderFingerprint();
const spiderVariable = "openlist-tvbox/internal/subscription.SpiderFingerprint";
const versionVariable = "openlist-tvbox/internal/buildinfo.Version";
const commitVariable = "openlist-tvbox/internal/buildinfo.Commit";
const sourceURLVariable = "openlist-tvbox/internal/buildinfo.SourceURL";
const sourceURL = process.env.SOURCE_URL || "https://github.com/outlook84/openlist-tvbox-gateway";

function gitValue(args, fallback) {
  const result = spawnSync("git", args, { encoding: "utf8" });
  if (result.status !== 0) return fallback;
  return result.stdout.trim() || fallback;
}

const version = process.env.VERSION || gitValue(["describe", "--tags", "--always", "--dirty"], "dev");
const commit = process.env.COMMIT || gitValue(["rev-parse", "--short=12", "HEAD"], "");
const ldflags = [
  `-X ${spiderVariable}=${fingerprint}`,
  `-X ${versionVariable}=${version}`,
  `-X ${commitVariable}=${commit}`,
  `-X ${sourceURLVariable}=${sourceURL}`,
].join(" ");

const result = spawnSync(
  "go",
  ["build", "-ldflags", ldflags, "-o", out, "./cmd/openlist-tvbox"],
  { stdio: "inherit" },
);

if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);

console.log(`Injected spider fingerprint: ${fingerprint}`);
console.log(`Injected version: ${version}`);
if (commit) console.log(`Injected commit: ${commit}`);
