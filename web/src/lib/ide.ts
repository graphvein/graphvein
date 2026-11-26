// The label of "Open in editor" buttons (T016e D6). The server reports
// the configured or preset label as ide.label; when it is null (the user's
// own link_template) the UI recognises the link's scheme, and anything it
// does not know reads "Open in editor". Shared by the API section and the
// code peek, so both name the editor the same way.

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import type { MarkKind } from "@/components/EditorMark";
import { editorName, presetOf, type EditorState } from "@/hooks/useEditor";

const SCHEME_LABEL: Record<string, string> = {
  vscode: "Open in VS Code",
  "vscode-insiders": "Open in VS Code Insiders",
  cursor: "Open in Cursor",
  vscodium: "Open in VSCodium",
  "vscodium-insiders": "Open in VSCodium Insiders",
  idea: "Open in IntelliJ IDEA",
  zed: "Open in Zed",
};

/** EDITOR_OFF is the reason a link is withheld when the browser chose
 * "No editor links" (ADR 0050). */
export const EDITOR_OFF = "Editor links are off in this browser (your choice)";

/** GENERIC_IDE_LABEL is the label when the editor is not known. */
export const GENERIC_IDE_LABEL = "Open in editor";

/** CHOOSE_EDITOR is the tooltip of an editor link while nothing chose an
 * editor: the first activation opens the chooser instead of a link. */
export const CHOOSE_EDITOR = "Choose an editor";

/** editorTip is the first line of every editor link's tooltip: the
 * editor the link opens and where to change it, or "Choose an editor"
 * while neither this browser nor the repository chose one (choosing).
 * label is the button's text ("Open in VS Code"). One function, so every
 * editor link says it the same way. */
export function editorTip(label: string, choosing: boolean): string {
  return choosing ? CHOOSE_EDITOR : `${label} · change in Settings`;
}

/** chooserNeeded tells whether the first activation of an editor link asks
 * for the editor: only when the preset list loaded, this browser applies
 * no choice (none stored, or a stored one this server does not offer) and
 * graphvein.yaml loads and sets no ide (no editor, no link_template). The
 * links then use Graphvein's built-in default, which nobody chose. */
export function chooserNeeded(state: EditorState): boolean {
  if (state.info.kind !== "ok" || state.choice !== null) return false;
  const repo = state.info.repository;
  return repo.available && repo.source === "builtin" && !repo.custom;
}

/** ideLabel names the editor of an IDE link from its scheme. */
export function ideLabel(link: string): string {
  const scheme = link.slice(0, Math.max(0, link.indexOf(":"))).toLowerCase();
  return Object.hasOwn(SCHEME_LABEL, scheme) ? (SCHEME_LABEL[scheme] ?? GENERIC_IDE_LABEL) : GENERIC_IDE_LABEL;
}

/** buttonLabel prefers the server's label, then the scheme of link, then
 * the generic label (no link: the button is disabled anyway). */
export function buttonLabel(label: string | null | undefined, link: string | null): string {
  if (label !== null && label !== undefined && label !== "") return label;
  return link === null ? GENERIC_IDE_LABEL : ideLabel(link);
}

// Presentation of one IDE button under the editor precedence (ADR 0050).
// Every word comes from the response's ide object and the server's preset
// list, never from the stored choice: a response built for another editor
// (an older server ignores the parameter) is named as what it is, with a
// notice.

/** sourceWords says in words which rule chose the editor of a
 * response; empty when an older server does not say. */
export function sourceWords(status: IdeStatus, name: string): string {
  if (status.available && (status.editor === null || status.editor === undefined) && status.source === "repository") {
    return "custom link from graphvein.yaml, not verified by Graphvein";
  }
  switch (status.source) {
    case "request":
      return `${name}, your choice in this browser`;
    case "repository":
      return `${name}, the repository default from graphvein.yaml`;
    case "builtin":
      return `${name}, Graphvein's default (graphvein.yaml sets no editor)`;
  }
  return "";
}

/** The caveat words of a preset status that is not "verified". */
export const STATUS_WORD: Record<string, string> = { partly_verified: "partly verified", limited: "limited" };

/** IdeView is everything a button shows: text, mark, link or the reason
 * it is disabled, title, accessible name and the visible caveat. */
export interface IdeView {
  text: string;
  mark: MarkKind | null;
  href: string | null;
  title: string;
  name: string;
  caveat: "warn" | "info" | null;
}

/** linkCaveat says why a link is not exact, or null when the generic
 * changed-file suffix (or nothing) applies. */
export function linkCaveat(link: IdeLink): string | null {
  switch (link.caveat) {
    case "tab_size_unknown":
      return "opens the line only: a tab precedes the node and the editor's tab size is not known";
    case "column_unit_assumed":
      return "column unit assumed from VS Code";
    case null:
      return link.line_only ? "opens the line only: the file changed since indexing; the line may have moved" : null;
    default:
      return "the column may be off";
  }
}

/** presentIde builds the view of one button. noLink is the reason when
 * the node has no link although links are available. */
export function presentIde(
  status: IdeStatus | null,
  link: IdeLink | null,
  editor: EditorState,
  noLink = "No IDE link for this node (no location)",
): IdeView {
  if (editor.off) {
    return { text: GENERIC_IDE_LABEL, mark: { kind: "off" }, href: null, title: EDITOR_OFF, name: EDITOR_OFF, caveat: null };
  }
  const text = buttonLabel(status?.label, link?.link ?? null);
  if (link === null) {
    const why = status !== null && !status.available ? `IDE links are unavailable: ${status.error ?? "configuration error"}` : noLink;
    return { text, mark: status !== null && !status.available ? { kind: "none" } : null, href: null, title: why, name: `${text}: ${why}`, caveat: null };
  }
  const preset = status?.editor ?? null;
  const name = preset !== null ? editorName(editor, preset, text) : text.replace(/^Open in /, "");
  const words = status !== null ? sourceWords(status, name) : "";
  const notes: string[] = [];
  let caveat: IdeView["caveat"] = null;
  const custom = status !== null && preset === null && status.source === "repository";
  if (custom) caveat = "warn";
  const p = presetOf(editor, preset);
  if (p !== undefined && p.status !== "verified") {
    const word = STATUS_WORD[p.status] ?? "verification unknown";
    notes.push(`${word}: ${p.note.replace(/\.$/, "")}`);
    caveat = p.status === "limited" ? "info" : "warn";
  }
  const asked = editor.requestEditor;
  if (typeof asked === "string" && (status?.source !== "request" || preset !== asked)) {
    notes.push("This server ignored your editor choice");
    caveat = "warn";
  }
  // A link the server did not vouch for (rule 14, ADR 0050 Review fixes
  // R2): the button says why in its name and title and shows a caveat icon.
  const why = linkCaveat(link);
  if (why !== null) {
    notes.push(why);
    caveat ??= "info";
  }
  const suffix = notes.length > 0 ? ` (${notes.join("; ")})` : "";
  const moved = link.exact || link.line_only || link.caveat !== null ? "" : " (the file changed; the line may have moved)";
  const mark: MarkKind | null =
    preset !== null ? { kind: "preset", editor: preset } : custom ? { kind: "custom", letters: name.slice(0, 2) || "?" } : null;
  return {
    text,
    mark,
    href: link.link,
    title: `${link.link}${moved}${words !== "" ? `\n${words}` : ""}${suffix}`,
    // "Open in Zed, your choice in this browser": the label already names
    // the editor, so the words drop it.
    name: `${text}${words !== "" ? `, ${words.startsWith(`${name}, `) ? words.slice(name.length + 2) : words}` : ""}${suffix}`,
    caveat,
  };
}
