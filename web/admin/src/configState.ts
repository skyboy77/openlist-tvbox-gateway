import type { AdminConfig, Backend } from "./types";
import type { EditorProps } from "./shared";

export const emptyConfig: AdminConfig = { backends: [], subs: [], tvbox: {} };

export function normalizeConfig(config: AdminConfig): AdminConfig {
  const backends = (config.backends || []).map(normalizeBackend);
  const backendTypeByID = Object.fromEntries(backends.map((backend) => [backend.id, backend.type || "openlist_v4"]));
  return {
    ...emptyConfig,
    ...config,
    tvbox: config.tvbox || {},
    backends,
    subs: (config.subs || []).map((sub) => ({
      ...sub,
      lives: sub.lives || [],
      mounts: (sub.mounts || []).map((mount) => (backendTypeByID[mount.backend] === "webdav" ? { ...mount, search: false, refresh: false } : mount)),
      access_code: "",
      access_code_hash_action: sub.access_code_hash_action || "keep",
    })),
  };
}

export function normalizeBackend(backend: Backend): Backend {
  const next = { ...backend, type: backend.type || "openlist_v4", auth_type: backend.auth_type || "anonymous" };
  if (next.type === "webdav" && next.auth_type === "api_key") next.auth_type = "anonymous";
  next.version = "";
  if (next.auth_type !== "api_key" || next.type === "webdav") {
    next.api_key = "";
    next.api_key_action = "clear";
  } else {
    next.api_key_action = next.api_key_action || "keep";
  }
  if (next.auth_type !== "password") {
    next.user = "";
    next.password = "";
    next.password_action = "clear";
  } else {
    next.password_action = next.password_action || "keep";
  }
  return next;
}

export function updateConfig(setConfig: EditorProps["setConfig"], patch: Partial<AdminConfig>) {
  setConfig((current) => ({ ...current, ...patch }));
}

export function updateTVBox(setConfig: EditorProps["setConfig"], patch: Partial<AdminConfig["tvbox"]>) {
  setConfig((current) => ({ ...current, tvbox: { ...(current.tvbox || {}), ...patch } }));
}
