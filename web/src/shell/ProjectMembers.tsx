// The project's member list in the sidebar (T022 B9, reshaped by the
// developer's decision of 2026-11-19): under `serve --project`
// the sidebar's "Repository" section lists the project's members with
// their freshness, the selected member highlighted, and the link pass
// state linking to the Project view. Clicking a member switches the
// repository every section reads (P8: one project per server, with a
// repository filter). The collapsed sidebar (icon rail) opens the same
// list in a popover. There is no project bar: the top bar is the
// single-repository one.
//
// The first member is selected only as a visible default: until a member
// is chosen the highlighted row says "shown by default: choose a
// repository" (label table row 30).

import { Boxes, CircleAlert, FolderGit2, Link2 } from "lucide-react";
import { useState } from "react";
import { Popover } from "radix-ui";

import type { LinkPass, ProjectMember } from "@/api/project";
import { useLabel } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { useProject, type ProjectState } from "@/shell/project";

/** freshnessWord words a member's freshness in plain words (label table
 * row 29, plain); anything but "fresh" warns. The Project view's members
 * table reads it too. */
export function freshnessWord(m: ProjectMember): string {
  return label("plain", "memberFreshness", freshnessInput(m)).text;
}

function freshnessInput(m: ProjectMember): { checkout: boolean; state: string; changed_count: number | null } {
  return { checkout: m.checkout, state: m.freshness.state, changed_count: m.freshness.changed_count };
}

/** memberTitle is a member row's tooltip: everything the list knows about
 * it. */
export function memberTitle(m: ProjectMember): string {
  const parts = [`${m.repository}: ${freshnessWord(m)}`];
  if (m.finished_at !== null) parts.push(`indexed ${formatShortTime(m.finished_at)}`);
  else parts.push("no stored index");
  if (!m.checkout) parts.push("no local checkout here: facts come from the stored index; code peek, editor links and freshness are unavailable");
  else if (m.path !== null) parts.push(`checkout ${m.path}`);
  if (m.freshness.error !== null && m.checkout) parts.push(m.freshness.error);
  const words = memberSettingsWords(m);
  if (words !== null) parts.push(words);
  parts.push(`${String(m.links.links)} project ${m.links.links === 1 ? "link" : "links"}${m.links.stale > 0 ? `, ${String(m.links.stale)} stale` : ""}`);
  if (m.links.index_error !== null) parts.push(`last project index failed: ${m.links.index_error}`);
  return parts.join("\n");
}

/** memberSettingsWords words a member's settings comparison for its
 * tooltip (row ST-48): "changed" names the keys; "unknown" (or a state
 * this UI does not know) says it is unknown; unchanged is said only here;
 * nothing for an absent comparison or a member without an index. */
export function memberSettingsWords(m: ProjectMember): string | null {
  const s = m.freshness.settings;
  if (s === null || s.state === "not_indexed") return null;
  if (s.state === "changed") return `settings changed: ${s.changed_keys.join(", ")}. Re-index to apply`;
  if (s.state === "unchanged") return "index settings unchanged since the last run";
  return `whether the index settings changed is unknown${s.reason !== null ? `: ${s.reason}` : ""}`;
}

/** passWord words the link pass state for the Project view's Links card. */
export function passWord(state: string, pass: number): string {
  if (state === "none") return "no link pass yet";
  return `links ${state} · pass ${String(pass)}`;
}

function MemberRow({ m, selected, explicit, onSelect }: { m: ProjectMember; selected: boolean; explicit: boolean; onSelect: () => void }) {
  const settingsChanged = m.freshness.settings?.state === "changed";
  const fresh = m.checkout && m.freshness.state === "fresh" && !settingsChanged;
  const freshness = useLabel("memberFreshness", freshnessInput(m));
  const note = useLabel("defaultMember", m.repository);
  return (
    <li>
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        title={memberTitle(m)}
        onClick={onSelect}
        data-testid="member-button"
        data-repository={m.repository}
        className={cn("flex h-7 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[13px] whitespace-nowrap", selected ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
      >
        <FolderGit2 aria-hidden="true" className="size-4 flex-none text-muted-foreground" />
        <span className="min-w-0 truncate">{m.repository}</span>
        <span
          aria-hidden="true"
          data-testid="member-dot"
          data-state={fresh ? "fresh" : settingsChanged && m.checkout && m.freshness.state === "fresh" ? "settings" : m.checkout ? "warn" : "none"}
          className={cn("ml-auto size-2 flex-none rounded-full", fresh ? "bg-linked" : m.checkout ? "bg-dynamic-bar" : "border border-muted-foreground")}
        />
        <span className="sr-only">
          , {freshness.text}
          {settingsChanged && m.checkout && ", settings changed"}
        </span>
      </button>
      {selected && !explicit && (
        <p className="m-0 pr-2 pb-0.5 pl-8 text-[11.5px] leading-snug text-dynamic italic" title={note.title} data-testid="default-member">
          {note.text}
        </p>
      )}
    </li>
  );
}

function LinkStatus({ pass }: { pass: LinkPass }) {
  const l = useLabel("linkStatus", pass);
  const fresh = pass.state === "fresh";
  return (
    <a
      href="#/project"
      title={`${l.title}\nProject view: members, links, declarations`}
      data-testid="link-pass"
      data-state={pass.state}
      className={cn("flex min-h-7 items-start gap-2 rounded-md px-2 py-1 text-[12px] leading-snug hover:bg-accent", fresh ? "text-muted-foreground" : "font-medium text-dynamic")}
    >
      {fresh ? <Link2 aria-hidden="true" className="mt-px size-3.5 flex-none" /> : <CircleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />}
      <span className="min-w-0">{l.text}</span>
    </a>
  );
}

/** MemberListView is the member list for a project state (tests pass
 * their own): the project name, one row per member, the link status. */
export function MemberListView({ state }: { state: ProjectState }) {
  const { project, repository, explicit, select } = state;
  return (
    <div className="grid gap-px" data-testid="project-members">
      <a href="#/project" className="flex h-[30px] min-w-0 items-center gap-2 px-2 text-xs font-semibold hover:underline" title={`Project ${project.project}: members, links, declarations`} data-testid="project-name">
        <Boxes aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
        <span className="min-w-0 truncate">{project.project}</span>
      </a>
      <ul role="radiogroup" aria-label="Repository" className="grid gap-px">
        {project.members.map((m) => (
          <MemberRow
            key={m.repository}
            m={m}
            selected={m.repository === repository}
            explicit={explicit}
            onSelect={() => {
              select(m.repository);
            }}
          />
        ))}
      </ul>
      <LinkStatus pass={project.link_pass} />
    </div>
  );
}

/** MemberRailButton is the collapsed sidebar's member list: an icon with
 * a popover holding the same list and link status. A dot on the icon
 * marks a default selection or stale links. */
export function MemberRailButton({ state }: { state: ProjectState }) {
  const [open, setOpen] = useState(false);
  const warn = !state.explicit || state.project.link_pass.state === "stale";
  const name = `Repository ${state.repository} in project ${state.project.project}`;
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          aria-label={name}
          title={name}
          data-testid="members-rail"
          className={cn("relative grid size-9 place-items-center rounded-md outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", open && "bg-accent ring-2 ring-ring/45")}
        >
          <FolderGit2 aria-hidden="true" className="size-4" />
          {warn && <span aria-hidden="true" className="absolute top-1 right-1 size-2 rounded-full bg-dynamic-bar" />}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="right"
          align="start"
          sideOffset={10}
          collisionPadding={8}
          aria-label="Repository"
          data-testid="members-popover"
          className="z-50 max-h-(--radix-popover-content-available-height) w-[248px] overflow-auto rounded-[calc(var(--radius)+2px)] border bg-popover p-1.5 text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
        >
          <MemberListView state={state} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** NoCheckoutNote says, above the section, that the selected member has
 * no local checkout here; nothing for a member with one or outside
 * project mode. */
export function NoCheckoutNote() {
  const state = useProject();
  const selected = state?.project.members.find((m) => m.repository === state.repository);
  if (selected === undefined || selected.checkout) return null;
  return (
    <p className="m-0 text-[12px] text-muted-foreground" data-testid="no-checkout">
      <b className="font-medium text-foreground">{selected.repository} has no local checkout here.</b> Everything shown comes from its stored index; code peek, editor links and freshness need its files and are unavailable.
    </p>
  );
}
