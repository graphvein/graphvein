// The editor preset list of the server's golden body, and helpers to
// build editor states and providers for tests (T016k).

import { parseIdeInfo, type IdeFetcher, type IdeInfo, type RepositoryIde } from "@/api/ide";
import type { EditorState } from "@/hooks/useEditor";
import builtin from "../../../internal/server/testdata/ide-builtin.golden.json";

/** ideInfo is the golden body, with the repository part replaced. */
export function ideInfo(repository: Partial<RepositoryIde> = {}): IdeInfo {
  const info = parseIdeInfo(structuredClone(builtin));
  return { ...info, repository: { ...info.repository, ...repository } };
}

/** ideFetcher answers with info. */
export function ideFetcher(info: IdeInfo = ideInfo()): IdeFetcher {
  return () => Promise.resolve(structuredClone(info));
}

/** editorState builds a state as the provider would. */
export function editorState(o: Partial<EditorState> & { repository?: Partial<RepositoryIde> } = {}): EditorState {
  const info = ideInfo(o.repository);
  const choice = o.choice ?? null;
  return {
    info: o.info ?? { kind: "ok", presets: info.presets, repository: info.repository },
    choice,
    ignored: o.ignored ?? null,
    saved: o.saved ?? true,
    remembered: o.remembered ?? true,
    requestEditor: o.requestEditor !== undefined ? o.requestEditor : choice === null || choice === "off" ? null : choice,
    off: choice === "off",
    setChoice: o.setChoice ?? (() => undefined),
    retry: () => undefined,
  };
}
