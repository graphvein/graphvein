// The Explorer's try-it state for the whole page (T029 part B, the
// developer's variant B): the environments of the repository or member,
// the selected one, and the decisions that go with a switch. Every guard
// is the server's (TI-8..TI-10, TI-12, TI-30, TI-48): this provider only
// asks the user and forwards the answer, so nothing here can let a
// request through that the server would refuse.
//
// Dialogs live here so any part of the page can open them: the production
// warning (one per switch to a production-labelled environment), adding an
// environment from a typed URL or a suggestion (preview, then one explicit
// confirmation), the environment settings (TLS pins, CA file, system
// proxy, limits; the same guarded edit), and a value typed for the
// session.

import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import type { EditAnswer, Environment, Environments, Pin, TryItClient } from "@/api/tryit";
import { useProject } from "@/shell/project";
import { AddEnvironmentDialog, ProductionDialog, SessionAuthDialog, SettingsDialog, type SettingsPreset } from "@/sections/explorer/tryit/dialogs";

export type AddPrefill = { url: string; name?: string; label?: string; source?: string | null; then?: (environmentId: string) => void };

type Dialog =
  | { kind: "add"; prefill: AddPrefill }
  | { kind: "settings"; env: Environment; preset: SettingsPreset | null; then: (() => void) | null }
  | { kind: "production"; env: Environment; then: (() => void) | null; reason?: string }
  | { kind: "session-auth"; env: Environment; then: (() => void) | null };

export interface TryItContextValue {
  client: TryItClient;
  envs: Environments | null;
  loadError: string | null;
  selected: Environment | null;
  /** Reads the environments again (the files are read on every request,
   * so an edit applies at once). */
  reload: () => Promise<Environments | null>;
  /** select switches the page to env (null: none). A production-labelled
   * environment first shows the warning; then runs after the server
   * recorded the switch. ask shows the warning even when the listing
   * says it was acknowledged (the server just said it was not). */
  select: (env: Environment | null, then?: () => void, ask?: boolean) => void;
  openAdd: (prefill: AddPrefill) => void;
  openSettings: (env: Environment, preset?: SettingsPreset | null, then?: () => void) => void;
  openSessionAuth: (env: Environment, then?: () => void) => void;
  clearSessionAuth: (env: Environment) => void;
  /** The last error of a switch or an edit, shown in the bar. */
  notice: string | null;
}

const TryItContext = createContext<TryItContextValue | null>(null);

/** useTryIt is the page's try-it state; null outside the Explorer. */
export function useTryIt(): TryItContextValue | null {
  return useContext(TryItContext);
}

/** errorText is an error's message for the UI. */
export function errorText(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return err instanceof Error ? err.message : String(err);
}

// Project mode keeps one selection per server process (TI-8), and the
// shell remounts the page when the member changes. The UI remembers the
// project environment the user chose (and acknowledged), so switching
// member selects the same project environment for the new member without a
// second dialog: the one production dialog covered every member it
// listed, and the server records an acknowledgement for each member as
// the UI selects it there. The memory keeps what the dialog listed
// ({member: base URL}) and the manifest's hash: a member that was not
// listed, a base URL that changed since, or any edit of the manifest asks
// again, so an acknowledgement never covers a destination the warning did
// not show.
interface ProjectChoice {
  project: string;
  id: string;
  acknowledged: boolean;
  /** The members and base URLs the production dialog listed. */
  listed: Record<string, string> | null;
  /** The manifest's hash when the choice was made. */
  manifest: string | null;
}
let projectChoice: ProjectChoice | null = null;

/** manifestHash is the project manifest's content hash in a listing. */
function manifestHash(list: Environments): string | null {
  return list.files.find((f) => f.target === "project")?.hash ?? null;
}

/** coveredBy says whether a remembered acknowledgement covers env for
 * member: the dialog listed this member with this base URL and the
 * manifest has not changed. Otherwise it says why the warning is shown
 * again. */
export function coveredBy(c: Pick<ProjectChoice, "listed" | "manifest">, member: string, env: Environment, list: Environments): { covered: true } | { covered: false; reason: string } {
  const listedUrl = c.listed?.[member];
  if (listedUrl === undefined) return { covered: false, reason: `The warning you confirmed did not list ${member}; ${env.name} sends its requests to ${env.baseUrl}.` };
  if (listedUrl !== env.baseUrl) return { covered: false, reason: `${env.name}'s base URL for ${member} changed since you confirmed: it was ${listedUrl}, it is now ${env.baseUrl}.` };
  if (c.manifest === null || c.manifest !== manifestHash(list)) return { covered: false, reason: "The project manifest changed since you confirmed this environment." };
  return { covered: true };
}

/** resetProjectChoice forgets the remembered project environment (tests). */
export function resetProjectChoice(): void {
  projectChoice = null;
}

/** editFile is the file an edit of target goes to, with its base hash. */
export function editFile(envs: Environments, target: string) {
  return envs.files.find((f) => f.target === target) ?? null;
}

/** pinsFor lists the pins an options edit keeps, in the API's form. */
export function pinsOf(env: Environment): Pin[] {
  return env.options.tls?.pins ?? [];
}

export function TryItProvider({ client, children, reloadKey }: { client: TryItClient; children: ReactNode; reloadKey: string }) {
  const project = useProject();
  const [envs, setEnvs] = useState<Environments | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const mounted = useRef(true);

  const reload = useCallback(async () => {
    try {
      const e = await client.environments({});
      if (mounted.current) {
        setEnvs(e);
        setLoadError(null);
      }
      return e;
    } catch (err) {
      if (mounted.current) setLoadError(errorText(err));
      return null;
    }
  }, [client]);

  const send = useCallback(
    async (env: Environment | null, acknowledge: boolean, list: Environments, listed: Record<string, string> | null = null) => {
      await client.select({ environment: env?.id ?? "", acknowledge_production: acknowledge }, list.token);
      if (project !== null) {
        projectChoice = env !== null && env.source === "project" ? { project: project.project.project, id: env.id, acknowledged: acknowledge, listed, manifest: manifestHash(list) } : null;
      }
      await reload();
    },
    [client, project, reload],
  );

  const select = useCallback(
    (env: Environment | null, then?: () => void, ask = false) => {
      if (envs === null) return;
      setNotice(null);
      if (env !== null && env.production && (ask || !(env.selected && env.acknowledged))) {
        setDialog({ kind: "production", env, then: then ?? null });
        return;
      }
      send(env, false, envs).then(
        () => {
          then?.();
        },
        (err: unknown) => {
          setNotice(errorText(err));
        },
      );
    },
    [envs, send],
  );

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    client.environments({ signal: controller.signal }).then(
      (e) => {
        setEnvs(e);
        setLoadError(null);
        // Project mode: the member changed (the page remounted); select
        // the project environment the user chose for the other member.
        const c = projectChoice;
        if (project === null || c === null || c.project !== project.project.project || e.selected !== null) return;
        const env = e.environments.find((x) => x.id === c.id);
        if (env === undefined || (env.production && !c.acknowledged)) return;
        if (env.production) {
          const cover = coveredBy(c, project.repository, env, e);
          if (!cover.covered) {
            setDialog({ kind: "production", env, then: null, reason: cover.reason });
            return;
          }
        }
        send(env, env.production, e, c.listed).catch((err: unknown) => {
          setNotice(errorText(err));
        });
      },
      (err: unknown) => {
        if (!controller.signal.aborted) setLoadError(errorText(err));
      },
    );
    return () => {
      mounted.current = false;
      controller.abort();
    };
    // reloadKey: a new index run or an applied edit reads the list again.
  }, [client, reloadKey, project, send]);

  const selected = envs?.environments.find((e) => e.selected) ?? null;

  const value: TryItContextValue = {
    client,
    envs,
    loadError,
    selected,
    reload,
    select,
    openAdd: (prefill) => {
      setDialog({ kind: "add", prefill });
    },
    openSettings: (env, preset = null, then) => {
      setDialog({ kind: "settings", env, preset, then: then ?? null });
    },
    openSessionAuth: (env, then) => {
      setDialog({ kind: "session-auth", env, then: then ?? null });
    },
    clearSessionAuth: (env) => {
      if (envs === null) return;
      client.auth({ environment: env.id, clear: true }, envs.token).then(
        async () => {
          await reload();
        },
        (err: unknown) => {
          setNotice(errorText(err));
        },
      );
    },
    notice,
  };

  const close = () => {
    setDialog(null);
  };

  // An options edit (pins, CA file, proxy) changes the environment's
  // identity: the server drops the selection, so it is selected again,
  // through the production warning when it is one.
  const afterOptions = async (env: Environment, then: (() => void) | null) => {
    const e = await reload();
    const now = e?.environments.find((x) => x.id === env.id);
    if (e === null || now === undefined) return;
    if (env.selected && !now.selected) {
      if (now.production) {
        setDialog({ kind: "production", env: now, then });
        return;
      }
      await send(now, false, e);
    }
    then?.();
  };

  return (
    <TryItContext.Provider value={value}>
      {children}
      {dialog?.kind === "production" && envs !== null && (
        <ProductionDialog
          env={dialog.env}
          client={client}
          reason={dialog.reason ?? null}
          onCancel={close}
          onConfirm={(listed) => {
            const then = dialog.then;
            close();
            send(dialog.env, true, envs, listed).then(
              () => {
                then?.();
              },
              (err: unknown) => {
                setNotice(errorText(err));
              },
            );
          }}
        />
      )}
      {dialog?.kind === "add" && envs !== null && (
        <AddEnvironmentDialog
          envs={envs}
          client={client}
          prefill={dialog.prefill}
          onCancel={close}
          onApplied={(answer: EditAnswer) => {
            const then = dialog.prefill.then;
            close();
            void reload().then((e) => {
              const env = e?.environments.find((x) => x.id === answer.environment);
              if (env === undefined || e === null) return;
              // The new environment is the one the user wants to use.
              if (env.production) {
                setDialog({ kind: "production", env, then: then !== undefined ? () => { then(env.id); } : null });
                return;
              }
              send(env, false, e).then(
                () => {
                  then?.(env.id);
                },
                (err: unknown) => {
                  setNotice(errorText(err));
                },
              );
            });
          }}
        />
      )}
      {dialog?.kind === "settings" && envs !== null && (
        <SettingsDialog
          env={dialog.env}
          envs={envs}
          client={client}
          preset={dialog.preset}
          onCancel={close}
          onApplied={() => {
            const { env, then } = dialog;
            close();
            afterOptions(env, then).catch((err: unknown) => {
              setNotice(errorText(err));
            });
          }}
        />
      )}
      {dialog?.kind === "session-auth" && envs !== null && (
        <SessionAuthDialog
          env={dialog.env}
          client={client}
          token={envs.token}
          onCancel={close}
          onSaved={() => {
            const then = dialog.then;
            close();
            void reload().then(() => {
              then?.();
            });
          }}
        />
      )}
    </TryItContext.Provider>
  );
}
