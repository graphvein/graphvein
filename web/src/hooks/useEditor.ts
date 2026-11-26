// The editor choice of this browser, shared by the picker, the data hooks
// and every IDE button (T016k, ADR 0050). The provider loads the preset
// list once (GET /api/v0/ide); data requests wait for it, so a stored value
// the server does not list is never sent as editor=.

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import type { EditorPreset, IdeFetcher, RepositoryIde } from "@/api/ide";
import { fetchIdeInfo } from "@/api/ide";
import { OFF, readChoice, resolveChoice, subscribeChoice, writeChoice } from "@/lib/editorChoice";

/** EditorState is what the UI knows about the editor of its links. */
export interface EditorState {
  /** The preset list: loading, answered, or failed with its message. */
  info: { kind: "loading" } | { kind: "ok"; presets: EditorPreset[]; repository: RepositoryIde } | { kind: "failed"; error: string };
  /** The applied choice: a listed preset, OFF, or null (repository default). */
  choice: string | null;
  /** A stored value that cannot apply (not listed, or too long). */
  ignored: string | null;
  /** False when browser storage is blocked: the choice lasts for this page. */
  saved: boolean;
  /** False when the choice was made for this page only (the first-click
   * chooser with "Remember for this browser" unchecked): storage was not
   * written, and the next page load asks again. */
  remembered: boolean;
  /** The editor parameter for data requests: undefined while the preset
   * list loads (requests wait), null for none. */
  requestEditor: string | null | undefined;
  /** "No editor links" is chosen: every IDE button is disabled. */
  off: boolean;
  /** Applies a choice at once: a preset name, OFF, or null for the
   * repository default (which removes the stored key). remember false
   * applies it to this page only and leaves storage as it is. */
  setChoice: (value: string | null, remember?: boolean) => void;
  /** Asks for the preset list again after a failure. */
  retry: () => void;
}

const noop = () => undefined;

/** Outside a provider (component tests) nothing is chosen and requests
 * go out at once without an editor parameter. */
const DEFAULT_STATE: EditorState = {
  info: { kind: "loading" },
  choice: null,
  ignored: null,
  saved: true,
  remembered: true,
  requestEditor: null,
  off: false,
  setChoice: noop,
  retry: noop,
};

const EditorContext = createContext<EditorState>(DEFAULT_STATE);

/** useEditor reads the editor state. */
export function useEditor(): EditorState {
  return useContext(EditorContext);
}

/** withEditor is the editor field of request parameters: absent when no
 * editor is chosen, so the URL carries no editor parameter then. */
export function withEditor(editor: string | null | undefined): { editor?: string } {
  return editor === null || editor === undefined ? {} : { editor };
}

/** presetOf finds a listed preset by name. */
export function presetOf(state: EditorState, editor: string | null | undefined): EditorPreset | undefined {
  if (state.info.kind !== "ok" || editor === null || editor === undefined) return undefined;
  return state.info.presets.find((p) => p.editor === editor);
}

/** editorName is the short editor name of a preset ("Zed"), from the
 * server's label; fallback is used when the preset is not listed. */
export function editorName(state: EditorState, editor: string | null | undefined, fallback?: string | null): string {
  const p = presetOf(state, editor);
  const label = p?.label ?? fallback ?? editor ?? "";
  return label.replace(/^Open in /, "");
}

/** EditorProvider owns the choice and the preset list. fetcher replaces
 * the request in tests. */
export function EditorProvider({ children, fetcher = fetchIdeInfo }: { children: ReactNode; fetcher?: IdeFetcher }) {
  const [info, setInfo] = useState<EditorState["info"]>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [stored, setStored] = useState<{ value: string | null; saved: boolean; page?: boolean }>(readChoice);

  useEffect(() => {
    const controller = new AbortController();
    fetcher({ signal: controller.signal }).then(
      (data) => {
        if (!controller.signal.aborted) setInfo({ kind: "ok", presets: data.presets, repository: data.repository });
      },
      (err: unknown) => {
        if (!controller.signal.aborted) setInfo({ kind: "failed", error: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => {
      controller.abort();
    };
  }, [fetcher, attempt]);

  useEffect(
    () =>
      subscribeChoice((value) => {
        setStored({ value, saved: true });
      }),
    [],
  );

  const setChoice = useCallback((value: string | null, remember = true) => {
    if (!remember) {
      setStored({ value, saved: true, page: true });
      return;
    }
    setStored({ value, saved: writeChoice(value) });
  }, []);
  const retry = useCallback(() => {
    setInfo({ kind: "loading" });
    setAttempt((a) => a + 1);
  }, []);

  const state = useMemo<EditorState>(() => {
    if (info.kind !== "ok") {
      // Without the list no preset can be checked, so no editor is sent and
      // the repository default applies (the picker shows the failure). A
      // stored "off" needs no list: it is still honoured, so a failed or
      // slow /api/v0/ide never turns links back on against the user's choice.
      const off = stored.value === OFF;
      return {
        info,
        choice: off ? OFF : null,
        ignored: null,
        saved: stored.saved,
        remembered: stored.page !== true,
        requestEditor: info.kind === "loading" ? undefined : null,
        off,
        setChoice,
        retry,
      };
    }
    const { choice, ignored } = resolveChoice(stored.value, info.presets.map((p) => p.editor));
    return {
      info,
      choice,
      ignored,
      saved: stored.saved,
      remembered: stored.page !== true,
      requestEditor: choice === null || choice === OFF ? null : choice,
      off: choice === OFF,
      setChoice,
      retry,
    };
  }, [info, stored, setChoice, retry]);

  return createElement(EditorContext.Provider, { value: state }, children);
}
