// The Project view (T022 B9; #/project, under `serve --project` only): the
// members (ID source, checkout, remote, freshness, link state), the links
// summary with the pass's outcome counts, the declarations or the "no
// declarations" explanation, the ambiguity explanations with their
// candidate providers, the stored in-repository links a link scope
// declaration contradicts ("conflicts with declaration") and the stale-link
// notice. Precision: an ambiguous call is never a link here, a stale link
// is shown with its reasons, and a conflicting stored link is flagged, not
// overridden (docs/extract/project-links.md).

import { CircleAlert, TriangleAlert } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

import type { AmbiguityGroup, NodeBrief, Project } from "@/api/project";
import { RepoBadge } from "@/components/RepoBadge";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { freshnessWord, passWord } from "@/shell/ProjectMembers";
import { useProject } from "@/shell/project";
import { ManifestEditor } from "@/sections/project/ManifestEditor";

/** DEFAULT_ASSUMPTION is the assumption of every link made without a
 * declaration (ADR 0075). */
export const DEFAULT_ASSUMPTION = "matched across project repositories; base URL not proven";

/** OUTCOME_ORDER lists outcome statuses in the order the view shows them. */
const OUTCOME_ORDER = ["linked", "ambiguous", "unmatched", "incomplete", "test_only", "unproven", "dynamic", "in_repository", "unknown"];

function Card({ title, note, children, testid }: { title: ReactNode; note?: ReactNode; children: ReactNode; testid: string }) {
  return (
    <section className="rounded-lg border bg-card" data-testid={testid}>
      <h2 className="m-0 flex flex-wrap items-baseline gap-x-2 border-b px-4 py-2 text-[13.5px] font-semibold">
        {title}
        {note !== undefined && <small className="text-[12px] font-normal text-muted-foreground">{note}</small>}
      </h2>
      <div className="px-4 py-3 text-[13px]">{children}</div>
    </section>
  );
}

function Place({ n }: { n: NodeBrief }) {
  if (n.file === "") return <span className="font-mono text-[12px] text-muted-foreground [overflow-wrap:anywhere]">{n.identity} (no longer stored)</span>;
  return (
    <span className="font-mono text-[12px] text-muted-foreground [overflow-wrap:anywhere]">
      {n.file}
      {n.line > 0 ? `:${String(n.line)}` : ""}
    </span>
  );
}

function NodeLine({ n, showRepo }: { n: NodeBrief; showRepo: boolean }) {
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5">
      {showRepo && <RepoBadge repository={n.repository} />}
      {n.name !== "" && <span className="font-mono text-[12.5px] [overflow-wrap:anywhere]">{n.name}</span>}
      <Place n={n} />
    </span>
  );
}

/** ambiguityText explains one group of ambiguous sources by its claim row
 * (docs/extract/project-links.md). */
/** capitalize starts a sentence built from reasons ("the manifest's
 * declarations changed ...") with a capital letter (T022 review). */
export function capitalize(text: string): string {
  return text === "" ? text : text.charAt(0).toUpperCase() + text.slice(1);
}

export function ambiguityText(g: AmbiguityGroup): string {
  const n = g.count === 1 ? "This source matches" : `These ${String(g.count)} sources match`;
  switch (g.row) {
    case "PL-R11":
      return `${n} routes in ${g.providers.length > 1 ? g.providers.join(" and ") : `several link scopes of ${g.providers.join("")}`}. Which server a call reaches is not proven, so no link is made. A consumes declaration in graphvein-project.yaml naming one provider (or a selector per provider) links them.`;
    case "PL-R10":
      return `${n} tied routes in ${g.providers.join(", ")}: the routes match equally well, so no link is made.`;
    case "PL-S3":
      return `${n} several routes inside ${g.providers.join(", ")}, and the link scope declarations do not decide between them, so the repository's own result (ambiguous) stands.`;
    default:
      return `${n} several routes (${g.providers.join(", ")}): no link is made.`;
  }
}

function AmbiguityCard({ g }: { g: AmbiguityGroup }) {
  const [open, setOpen] = useState(false);
  const style = useLabelStyle();
  const row = label(style, "claimRow", g.row);
  return (
    <li className="grid gap-1.5 border-b py-2 last:border-b-0" data-testid="ambiguity-group" data-row={g.row}>
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="font-semibold">{g.count}</span>
        <span className="text-muted-foreground">ambiguous {g.count === 1 ? "source" : "sources"} · candidate providers</span>
        {g.providers.map((p) => (
          <RepoBadge key={p} repository={p} />
        ))}
        {row.text !== "" && (
          <span className="font-mono text-[11.5px] text-muted-foreground" title={row.title}>
            {row.text}
          </span>
        )}
      </div>
      <p className="m-0 text-[12.5px] text-muted-foreground" title={row.title}>
        {ambiguityText(g)}
      </p>
      <button
        type="button"
        aria-expanded={open}
        className="w-fit text-[12.5px] font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        {open ? "Hide the sources" : `Show ${g.sources.length < g.count ? `the first ${String(g.sources.length)} of ${String(g.count)}` : g.count === 1 ? "the source" : `the ${String(g.count)} sources`}`}
      </button>
      {open && (
        <ul className="grid gap-2 pl-3">
          {g.sources.map((s) => (
            <li key={`${s.source.repository}\u0000${s.source.identity}\u0000${s.template}`} className="grid gap-0.5" data-testid="ambiguous-source">
              <NodeLine n={s.source} showRepo />
              {s.template !== "" && <span className="font-mono text-[12px]">{s.template}</span>}
              <span className="text-[12px] text-muted-foreground">
                {s.candidate_count} candidate {s.candidate_count === 1 ? "route" : "routes"}
                {s.candidates.length < s.candidate_count ? ` (first ${String(s.candidates.length)} listed)` : ""}:
              </span>
              <ul className="grid gap-0.5 pl-3">
                {s.candidates.map((c) => (
                  <li key={`${c.repository}\u0000${c.identity}`}>
                    <NodeLine n={c} showRepo />
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

/** ProjectView renders a loaded project document. */
export function ProjectView({ project, repository }: { project: Project; repository: string }) {
  const style = useLabelStyle();
  const pass = project.link_pass;
  const decl = project.declarations;
  const outcomes = OUTCOME_ORDER.filter((s) => (pass.outcomes[s] ?? 0) > 0).concat(Object.keys(pass.outcomes).filter((s) => !OUTCOME_ORDER.includes(s)));
  return (
    <div className="grid max-w-[1100px] grid-cols-[minmax(0,1fr)] gap-3.5" data-testid="project-view">
      <div>
        <h1 className="m-0 text-lg font-semibold">Project {project.project}</h1>
        <p className="m-0 text-[13px] text-muted-foreground">
          {project.members.length} {project.members.length === 1 ? "member" : "members"}, linked across repositories by the stored link pass. The sections show one repository at a time: {repository} now.
        </p>
      </div>
      {pass.state === "stale" && (
        <div role="status" className="flex gap-2 rounded-lg border border-dynamic-bar bg-dynamic-soft px-4 py-2.5 text-[13px]" data-testid="stale-notice">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none text-dynamic" />
          <div>
            <b>The project links are stale.</b> {capitalize(pass.stale_reasons.join("; "))}. Stale links are still shown and followed, each marked stale, until <code className="font-mono">graphvein index --project</code> (or Re-index) runs the link pass again.
          </div>
        </div>
      )}
      {pass.state === "none" && (
        <div role="status" className="flex gap-2 rounded-lg border border-dynamic-bar bg-dynamic-soft px-4 py-2.5 text-[13px]" data-testid="no-pass-notice">
          <CircleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none text-dynamic" />
          <div>
            <b>No link pass is stored yet.</b> Run <code className="font-mono">graphvein index --project</code> (or Re-index): until then no call is linked across repositories.
          </div>
        </div>
      )}
      <Card title="Members" testid="project-members">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] border-collapse text-left text-[12.5px]">
            <thead className="text-muted-foreground">
              <tr className="border-b">
                <th className="py-1 pr-3 font-medium">Repository</th>
                <th className="py-1 pr-3 font-medium">Checkout</th>
                <th className="py-1 pr-3 font-medium">Freshness</th>
                <th className="py-1 pr-3 font-medium">Indexed</th>
                <th className="py-1 pr-3 font-medium">Project links</th>
              </tr>
            </thead>
            <tbody>
              {project.members.map((m) => (
                <tr key={m.repository} className="border-b align-top last:border-b-0" data-testid="project-member">
                  <td className="py-1.5 pr-3">
                    <RepoBadge repository={m.repository} />
                    <div className="mt-0.5 text-[11.5px] text-muted-foreground">ID from {m.repository_source}</div>
                  </td>
                  <td className="py-1.5 pr-3">
                    {m.checkout ? <span className="font-mono [overflow-wrap:anywhere]">{m.path ?? "(no path)"}</span> : <span className="text-dynamic">no local checkout here</span>}
                    {m.remote_url !== null && <div className="font-mono text-[11.5px] text-muted-foreground [overflow-wrap:anywhere]">{m.remote_url}</div>}
                  </td>
                  <td className={cn("py-1.5 pr-3", m.checkout && m.freshness.state === "fresh" ? "" : "text-dynamic")}>{freshnessWord(m)}</td>
                  <td className="py-1.5 pr-3">{m.finished_at !== null ? formatShortTime(m.finished_at) : "not indexed"}</td>
                  <td className="py-1.5 pr-3">
                    {m.links.links} {m.links.stale > 0 && <span className="text-dynamic">({m.links.stale} stale)</span>}
                    {!m.links.in_pass && pass.state !== "none" && <div className="text-[11.5px] text-dynamic">not in the last link pass</div>}
                    {m.links.index_error !== null && <div className="text-[11.5px] text-dynamic">last project index failed: {m.links.index_error}</div>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {project.checks.map((c) => (
          <p key={c.check + c.members.join()} className="m-0 mt-2 text-[12.5px] text-dynamic" data-testid="project-check">
            {c.message}
          </p>
        ))}
      </Card>
      <Card title="Links" note={passWord(pass.state, pass.pass)} testid="project-links">
        <p className="m-0">
          {pass.links} cross-repository {pass.links === 1 ? "link" : "links"}
          {pass.stale_links > 0 ? `, ${String(pass.stale_links)} stale` : ""}. Every link is inferred, never more confident than the match inside one repository, and shows the assumption it rests on.
        </p>
        {outcomes.length > 0 && (
          <ul className="m-0 mt-2 flex list-none flex-wrap gap-1.5 p-0" aria-label="Link pass outcomes">
            {outcomes.map((s) => {
              const l = label(style, "outcomeStatus", s);
              return (
                <li key={s} className="inline-flex h-6 items-center gap-1.5 rounded-md border px-2 text-[12px]" title={l.title} data-testid="outcome-count" data-status={s}>
                  <span className="font-semibold tabular-nums">{pass.outcomes[s]}</span>
                  <span className={cn(style === "technical" && "font-mono")}>{l.text}</span>
                </li>
              );
            })}
          </ul>
        )}
      </Card>
      <Card title="Declarations" note="graphvein-project.yaml" testid="project-declarations">
        {decl.consumes.length === 0 ? (
          <p className="m-0" data-testid="no-declarations">
            <b>No consumes declarations.</b> Every member&apos;s routes are candidates for every member&apos;s calls. A call that matches routes in exactly one other repository is linked, with the assumption “{DEFAULT_ASSUMPTION}”; a call that matches routes in several repositories stays ambiguous and is not linked. Declaring which repository a member consumes (<code className="font-mono">consumes</code>, with selectors per provider) narrows the candidates.
          </p>
        ) : (
          <ul className="m-0 grid list-none gap-1 p-0">
            {decl.consumes.map((c, i) => (
              <li key={`${c.consumer}\u0000${String(i)}`} className="font-mono text-[12.5px] [overflow-wrap:anywhere]" data-testid="declaration">
                {c.text.replace(/`/g, "")}
              </li>
            ))}
          </ul>
        )}
        {decl.scopes.length > 0 && (
          <div className="mt-2 text-[12.5px]">
            <span className="text-muted-foreground">Link scopes: </span>
            {decl.scopes.map((s, i) => (
              <span key={`${s.repository}/${s.name}`} className="font-mono">
                {i > 0 ? ", " : ""}
                {s.repository}/{s.name} ({s.path})
              </span>
            ))}
          </div>
        )}
      </Card>
      <Card title="Ambiguous calls" note="never a link" testid="project-ambiguity">
        {project.ambiguous.total === 0 ? (
          <p className="m-0 text-muted-foreground">The last link pass left no source ambiguous.</p>
        ) : (
          <ul className="m-0 grid list-none p-0">
            {project.ambiguous.groups.map((g) => (
              <AmbiguityCard key={`${g.row}\u0000${g.providers.join()}`} g={g} />
            ))}
          </ul>
        )}
      </Card>
      {(project.conflicts.length > 0 || decl.scopes.length > 0) && (
        <Card title="Conflicts with declaration" note="flagged, never overridden" testid="project-conflicts">
          {project.conflicts.length === 0 ? (
            <p className="m-0 text-muted-foreground">No call linked inside its repository contradicts a link scope declaration.</p>
          ) : (
            <ul className="m-0 grid list-none gap-2 p-0">
              {project.conflicts.map((c) => (
                <li key={`${c.call.identity}\u0000${c.target.identity}`} className="grid gap-0.5 border-b pb-2 last:border-b-0" data-testid="conflict">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="inline-flex h-5 items-center rounded-[5px] border border-dynamic-bar px-1.5 text-[11.5px] font-medium text-dynamic">conflicts with declaration</span>
                    <NodeLine n={c.call} showRepo />
                  </div>
                  <p className="m-0 text-[12.5px]">
                    Linked inside {c.call.repository} to <span className="font-mono">{c.target.name !== "" ? c.target.name : c.target.identity}</span> in {c.target_scope !== "" ? `scope ${c.call.repository}/${c.target_scope}` : `the default scope of ${c.call.repository}`} (<Place n={c.target} />), while {c.consumer} declares: {c.declared.map((d) => d.replace(/`/g, "")).join("; ")}. The stored link is kept; check the declaration or the call.
                  </p>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
    </div>
  );
}

/** ProjectSection is the Project view in the shell; it asks for a fresh
 * project document when shown. */
export function ProjectSection() {
  const state = useProject();
  const refresh = state?.refresh;
  useEffect(() => {
    refresh?.();
  }, [refresh]);
  if (state === null) return null;
  return (
    <div className="grid max-w-[1100px] gap-3.5">
      <ProjectView project={state.project} repository={state.repository} />
      <ManifestEditor onWritten={state.refresh} />
    </div>
  );
}
