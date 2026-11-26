// The label style of this browser (T025b): the "Label style" row of
// Settings (moved from the sidebar footer, T039), the first-run banner, and every T025 string read
// through lib/labels.ts. Precedence: (1) the browser's stored choice;
// (2) the repository default, graphvein.yaml's ui.labels (GET /api/v0/ui);
// (3) plain.

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { fetchUiDefaults, type UiFetcher } from "@/api/ui";
import { parseDepth } from "@/lib/depthChoice";
import { parseStyle, readLabels, subscribeLabels, writeBannerDismissed, writeStyle, type LabelStyle, type StoredLabels } from "@/lib/labelChoice";
import { label, type InputOf, type Label, type LabelKey } from "@/lib/labels";

/** Where the applied style comes from. */
export type LabelSource = "browser" | "repository" | "default";

/** LabelState is what the UI knows about its label style. */
export interface LabelState {
  /** The applied style. */
  style: LabelStyle;
  source: LabelSource;
  /** False when browser storage is blocked: a choice lasts for this page. */
  saved: boolean;
  /** The first-run banner shows: nothing is chosen in this browser, the
   * repository sets no default, and the banner was not closed. */
  banner: boolean;
  /** Chooses a style in this browser (also closes the banner); null clears
   * the browser's choice, so the repository default applies again. */
  setStyle: (style: LabelStyle | null) => void;
  /** Closes the banner without choosing: the current style stays. */
  dismissBanner: () => void;
  /** The repository default from graphvein.yaml: undefined while loading,
   * null when none is set (or it could not be read). */
  repository: LabelStyle | null | undefined;
  /** Reads the repository default again (after Settings wrote
   * graphvein.yaml). */
  refresh: () => void;
}

const noop = () => undefined;

/** Outside a provider (component tests) labels are plain and no banner
 * shows, which is the behaviour before T025b. */
const DEFAULT_STATE: LabelState = { style: "plain", source: "default", saved: true, banner: false, setStyle: noop, dismissBanner: noop, repository: null, refresh: noop };

const LabelContext = createContext<LabelState>(DEFAULT_STATE);

/** useLabels reads the label state. */
export function useLabels(): LabelState {
  return useContext(LabelContext);
}

/** useLabel renders one table entry in the applied style. */
export function useLabel<K extends LabelKey>(key: K, input: InputOf<K>): Label {
  return label(useContext(LabelContext).style, key, input);
}

/** useLabelStyle is the applied style, for code that renders several
 * labels. */
export function useLabelStyle(): LabelStyle {
  return useContext(LabelContext).style;
}

/** The repository default: unknown until GET /api/v0/ui answers. */
type RepoDefault = { kind: "loading" } | { kind: "done"; style: LabelStyle | null; impact: number | null; chain: number | null };

/** RepoDepths are the repository's depth defaults, graphvein.yaml's
 * ui.impact_depth and ui.chain_depth (T042), read from the same
 * GET /api/v0/ui answer as ui.labels: unknown until it answers, null
 * when not set (or the request failed). */
export type RepoDepths = { loaded: false } | { loaded: true; impact: number | null; chain: number | null };

/** Outside a provider (component tests) no repository default is set. */
const RepoDepthContext = createContext<RepoDepths>({ loaded: true, impact: null, chain: null });

/** useRepoDepths reads the repository's depth defaults. */
export function useRepoDepths(): RepoDepths {
  return useContext(RepoDepthContext);
}

/** LabelProvider owns the stored choice and the repository default.
 * fetcher replaces the request in tests. */
export function LabelProvider({ children, fetcher = fetchUiDefaults }: { children: ReactNode; fetcher?: UiFetcher }) {
  const [stored, setStored] = useState<StoredLabels>(readLabels);
  const [repo, setRepo] = useState<RepoDefault>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    fetcher({ signal: controller.signal }).then(
      (data) => {
        if (!controller.signal.aborted) setRepo({ kind: "done", style: parseStyle(data.labels), impact: parseDepth(data.impact_depth), chain: parseDepth(data.chain_depth) });
      },
      () => {
        // No repository default when the request fails: plain applies.
        if (!controller.signal.aborted) setRepo({ kind: "done", style: null, impact: null, chain: null });
      },
    );
    return () => {
      controller.abort();
    };
  }, [fetcher, attempt]);

  useEffect(
    () =>
      subscribeLabels(() => {
        setStored(readLabels());
      }),
    [],
  );

  const setStyle = useCallback((style: LabelStyle | null) => {
    const saved = writeStyle(style);
    setStored((s) => ({ ...s, style, saved }));
  }, []);
  const dismissBanner = useCallback(() => {
    const saved = writeBannerDismissed();
    setStored((s) => ({ ...s, bannerDismissed: true, saved: s.saved && saved }));
  }, []);

  const refresh = useCallback(() => {
    setAttempt((a) => a + 1);
  }, []);

  const state = useMemo<LabelState>(() => {
    const repoStyle = repo.kind === "done" ? repo.style : null;
    const style = stored.style ?? repoStyle ?? "plain";
    const source: LabelSource = stored.style !== null ? "browser" : repoStyle !== null ? "repository" : "default";
    // The banner waits for the repository default, so it never flashes
    // for a repository that set one.
    const banner = stored.style === null && !stored.bannerDismissed && repo.kind === "done" && repo.style === null;
    return { style, source, saved: stored.saved, banner, setStyle, dismissBanner, repository: repo.kind === "done" ? repo.style : undefined, refresh };
  }, [stored, repo, setStyle, dismissBanner, refresh]);

  const depths = useMemo<RepoDepths>(() => (repo.kind === "done" ? { loaded: true, impact: repo.impact, chain: repo.chain } : { loaded: false }), [repo]);

  return createElement(RepoDepthContext.Provider, { value: depths }, createElement(LabelContext.Provider, { value: state }, children));
}
