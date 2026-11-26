// The coverage table of the Evidence popover (T020, Variant B's table as
// the popover's content): languages from /precision `files` as rows (file
// counts by Graphvein's file label, run refusals applied, no percentage,
// O12 a), the labelled fact
// families from `rules` and `default.families` as columns, and in each
// cell the facts this index holds of that family in that language's
// files. A family's label is the server's (`label` of its row). A cell
// whose count is 0 is dimmed: this index holds no such fact there, so a
// Precise label guarantees nothing in it. Families with no fact in this
// index are named under the table instead of taking a column.
//
// External runs (O6 a): files by the label the producer reported
// (`reported_by_label`, never `by_label`), marked "not checked"; no family
// is labelled.

import { cn } from "@/lib/utils";
import type { Facts, PrecisionResponse } from "@/api/precision";
import { LabelTag, type Focus } from "@/components/precision/parts";
import { familyName } from "@/components/precision/words";

/** Column is one labelled fact family. */
export interface Column {
  /** A rule row id ("PL-F2") or a default family ("routes"). */
  key: string;
  name: string;
  /** The row the label comes from. */
  row: string;
  precise: boolean;
  facts: Facts | null;
}

/** columnsOf lists every labelled family the server sends, rule rows
 * first, in the server's order. */
export function columnsOf(data: PrecisionResponse): Column[] {
  return [
    ...data.rules.map((r) => ({ key: r.id, name: r.scope, row: r.id, precise: r.label === "precise", facts: r.facts })),
    ...data.default.families.map((f) => ({ key: f.family, name: familyName(f.family), row: data.default.row, precise: f.label === "precise", facts: f.facts })),
  ];
}

/** Layout is what the table shows: the columns with facts (and the
 * focused one), the families without facts, and the language rows with
 * the languages that hold no fact of a shown column folded into one. */
export interface Layout {
  shown: Column[];
  empty: Column[];
  rows: { language: string; files: number | null; byLabel: Record<string, number> }[];
  filesOnly: { language: string; files: number; byLabel: Record<string, number> }[];
}

export function layoutOf(data: PrecisionResponse, focus: Focus): Layout {
  const all = columnsOf(data);
  const shown = all.filter((c) => (c.facts?.total ?? 0) > 0 || c.key === focus.column);
  const empty = all.filter((c) => !shown.includes(c));
  const files = data.files?.languages ?? [];
  const known = new Set(files.map((l) => l.language));
  const has = (lang: string) => shown.some((c) => (c.facts?.by_language[lang] ?? 0) > 0);
  const rows: Layout["rows"] = [];
  const filesOnly: Layout["filesOnly"] = [];
  for (const l of files) {
    if (has(l.language) || l.language === focus.language) rows.push({ language: l.language, files: l.total, byLabel: l.by_label });
    else filesOnly.push({ language: l.language, files: l.total, byLabel: l.by_label });
  }
  // Facts whose file has no stored record ("none"), or any language the
  // file counts do not list, still get a row.
  for (const c of shown) {
    for (const lang of Object.keys(c.facts?.by_language ?? {})) {
      if (!known.has(lang)) {
        known.add(lang);
        rows.push({ language: lang, files: null, byLabel: {} });
      }
    }
  }
  return { shown, empty, rows, filesOnly };
}

const exact = new Intl.NumberFormat(undefined);

/** formatCount shows a count in full: the table states what the store
 * holds, so it never rounds. */
function formatCount(n: number): string {
  return exact.format(n);
}

const LABEL_WORD: Record<string, string> = { precise: "Precise", best_effort: "Best effort" };

/** labelCounts is a language's files by Graphvein's label in words. */
export function labelCounts(byLabel: Record<string, number>, total: number | null): string {
  const parts = Object.entries(byLabel).filter(([, n]) => n > 0);
  if (parts.length === 1 && total !== null && parts[0]?.[1] === total) return LABEL_WORD[parts[0][0]] ?? parts[0][0];
  return parts.map(([k, n]) => `${formatCount(n)} ${LABEL_WORD[k] ?? k}`).join(", ");
}

/** filesOnlyText is the folded row's file counts: "22, 1, 4 Best effort"
 * when every language has one and the same label, else each language's
 * counts in words. */
export function filesOnlyText(list: readonly { files: number; byLabel: Record<string, number> }[]): string {
  const words = list.map((l) => labelCounts(l.byLabel, l.files));
  const first = words[0] ?? "";
  if (words.every((w) => w === first && w in REVERSE_LABEL)) return `${list.map((l) => formatCount(l.files)).join(", ")} ${first}`;
  return list.map((l, i) => `${formatCount(l.files)} (${words[i] ?? ""})`).join(", ");
}

const REVERSE_LABEL: Record<string, string> = { Precise: "precise", "Best effort": "best_effort" };

const TH = "border-b bg-muted/50 px-1.5 py-1.5 text-left align-bottom text-[11.5px] leading-[1.25] font-medium text-muted-foreground";
const TD = "border-b px-1.5 py-0.5 align-top";

function ReportedTable({ data, focus }: { data: PrecisionResponse; focus: Focus }) {
  const files = data.files?.languages ?? [];
  const by = data.files?.reported_by ?? data.index?.indexer.name ?? "the external indexer";
  return (
    <>
      <h4 className="m-0 mt-1 text-[12.5px] font-semibold">
        Files in this index <small className="font-normal text-muted-foreground">by the label {by} reported</small>
      </h4>
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full border-collapse text-[12px] [&_tr:last-child_td]:border-b-0" data-testid="precision-coverage" data-mode="reported">
          <thead>
            <tr>
              <th className={TH}>Language</th>
              <th className={cn(TH, "text-right")}>Files</th>
              <th className={TH}>Label reported by {by}</th>
            </tr>
          </thead>
          <tbody>
            {files.map((l) => (
              <tr key={l.language} className={cn(l.language === focus.language && "bg-brand-soft/55")} data-language={l.language}>
                <td className={cn(TD, "font-mono")}>{l.language}</td>
                <td className={cn(TD, "text-right tabular-nums")}>{formatCount(l.total)}</td>
                <td className={cn(TD, "text-other")}>
                  {Object.entries(l.reported_by_label ?? {})
                    .map(([k, n]) => `${formatCount(n)} ${k}`)
                    .join(", ")}{" "}
                  (not checked)
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="m-0 text-[11.5px] text-muted-foreground">No fact family is labelled in an external run. Counts are files, not a score: no percentage is shown.</p>
    </>
  );
}

/** CoverageTable renders the coverage of this index. */
export function CoverageTable({ data, focus }: { data: PrecisionResponse; focus: Focus }) {
  if (data.run?.refusal === "external_indexer") return <ReportedTable data={data} focus={focus} />;
  if (data.files === null) return <p className="m-0 text-muted-foreground">Nothing is indexed yet, so there is no coverage to show.</p>;
  const { shown, empty, rows, filesOnly } = layoutOf(data, focus);
  return (
    <>
      <h4 className="m-0 mt-1 flex flex-wrap items-baseline gap-x-2 text-[12.5px] font-semibold">
        Coverage in this index
        <small className="font-normal text-muted-foreground">files per language by Graphvein&apos;s file label; facts per labelled family by the language of their file</small>
      </h4>
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full border-collapse text-[12px] [&_tr:last-child_td]:border-b-0" data-testid="precision-coverage" data-mode="labelled">
          <thead>
            <tr>
              <th className={cn(TH, "w-[96px]")}>Language</th>
              <th className={cn(TH, "w-[150px]")}>Files</th>
              {shown.map((c) => (
                <th key={c.key} className={cn(TH, "min-w-[104px]", c.key === focus.column && "bg-brand-soft/55 text-brand-text")} data-column={c.key}>
                  <span className="block">{c.name}</span>
                  <span className="mt-1 flex flex-wrap items-center gap-1">
                    <LabelTag precise={c.precise}>{c.precise ? "Precise" : "Best effort"}</LabelTag>
                    <span className="font-mono text-[10.5px]">{c.row}</span>
                  </span>
                  <span className="block text-[10.5px] font-normal">{c.facts?.unit ?? "facts"}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.language} className={cn(r.language === focus.language && "bg-brand-soft/55")} data-language={r.language}>
                <td className={cn(TD, "font-mono")}>{r.language === "none" ? <span title="The fact's file has no stored file record">none</span> : r.language}</td>
                <td className={TD}>
                  {r.files === null ? (
                    <span className="text-muted-foreground">no file record</span>
                  ) : (
                    <>
                      <span className="tabular-nums">{formatCount(r.files)}</span> <span className="text-muted-foreground">{labelCounts(r.byLabel, r.files)}</span>
                    </>
                  )}
                </td>
                {shown.map((c) => {
                  const n = c.facts?.by_language[r.language] ?? 0;
                  const hl = c.key === focus.column;
                  return n > 0 ? (
                    <td key={c.key} className={cn(TD, "tabular-nums", hl && "bg-brand-soft/55", c.precise && "font-semibold text-brand-text")} data-cell={`${r.language}:${c.key}`}>
                      {formatCount(n)}
                    </td>
                  ) : (
                    <td
                      key={c.key}
                      className={cn(TD, "text-muted-foreground/70", hl && "bg-brand-soft/55")}
                      title="No fact of this family in this language's files in this index: the label guarantees nothing here."
                      data-cell={`${r.language}:${c.key}`}
                      data-empty="true"
                    >
                      0
                    </td>
                  );
                })}
              </tr>
            ))}
            {filesOnly.length > 0 && (
              <tr data-language="files-only">
                <td className={cn(TD, "font-mono whitespace-normal")}>{filesOnly.map((l) => l.language).join(", ")}</td>
                <td className={cn(TD, "whitespace-normal")}>{filesOnlyText(filesOnly)}</td>
                <td className={cn(TD, "text-muted-foreground/70")} colSpan={Math.max(shown.length, 1)}>
                  no fact of a labelled family in these files (file labels only)
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="m-0 text-[11.5px] text-muted-foreground" data-testid="precision-coverage-note">
        A dimmed 0: this index holds no fact of that family in that language&apos;s files, so its label guarantees nothing there. Counts are files and facts, not a score: no
        percentage is shown.
        {empty.length > 0 && (
          <>
            {" "}
            No facts in this index:{" "}
            {empty.map((c, i) => (
              <span key={c.key}>
                {i > 0 && "; "}
                {c.name} ({c.precise ? "Precise" : "Best effort"}, {c.row})
              </span>
            ))}
            .
          </>
        )}
      </p>
    </>
  );
}
