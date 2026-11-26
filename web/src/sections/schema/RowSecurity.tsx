import { rawText } from "@/api/schema";

/** flagText renders one rls flag: a boolean as stored, null as unknown. */
function flagText(v: unknown, on: string, off: string): { text: string; unknown: boolean } {
  if (v === true) return { text: on, unknown: false };
  if (v === false) return { text: off, unknown: false };
  if (v === null) return { text: "unknown", unknown: true };
  return { text: rawText(v), unknown: true };
}

/** RowSecurity shows a Supabase table's row level security (T024c1, row
 * SB-M3): the rls flags (enabled, forced) and its policies as written,
 * name, command and table. Nothing is claimed about what a policy
 * permits, so its roles, USING and WITH CHECK are not shown. It renders
 * nothing for a table whose record has no rls attribute (every other
 * source). */
export function RowSecurity({ attrs, completeness }: { attrs: Record<string, unknown>; completeness: string }) {
  if (!Object.hasOwn(attrs, "rls")) return null;
  const rls = attrs.rls;
  const flags = rls !== null && typeof rls === "object" && !Array.isArray(rls) ? (rls as Record<string, unknown>) : null;
  const enabled = flags === null ? { text: "unknown", unknown: true } : flagText(flags.enabled, "enabled", "disabled");
  const forced = flags === null ? { text: "unknown", unknown: true } : flagText(flags.forced, "forced", "not forced");
  const policies = attrs.policies;
  const complete = completeness === "complete";
  return (
    <section className="grid gap-2 border-t px-5 py-3.5" data-testid="row-security-section">
      <h3 className="m-0 flex flex-wrap items-baseline gap-2 text-[14px] font-semibold">
        Row level security
        <span className="text-[12px] font-normal text-muted-foreground">as written in the migrations · what a policy permits is not read</span>
      </h3>
      <p className="m-0 text-[13px]" data-testid="rls-flags">
        <span className={enabled.unknown ? "text-muted-foreground italic" : "font-semibold"}>{enabled.text}</span>
        <span className="text-muted-foreground"> · </span>
        <span className={forced.unknown ? "text-muted-foreground italic" : ""}>{forced.text}</span>
        {(enabled.unknown || forced.unknown) && <span className="text-muted-foreground"> (a statement the pass does not read may have set it)</span>}
      </p>
      {Array.isArray(policies) ? (
        policies.length === 0 ? (
          <p className="m-0 text-[13px] text-muted-foreground" data-testid="policies-none">
            {complete ? "No policy." : "No policy the migrations create; the table is not complete, so others may exist."}
          </p>
        ) : (
          <>
            <table className="w-full border-collapse text-[13px]" data-testid="policies">
              <thead>
                <tr className="text-left text-[12px] text-muted-foreground">
                  <th className="py-1 pr-3 font-normal">Policy</th>
                  <th className="py-1 pr-3 font-normal">Command</th>
                  <th className="py-1 font-normal">On</th>
                </tr>
              </thead>
              <tbody>
                {(policies as unknown[]).map((p, i) => {
                  const o = p !== null && typeof p === "object" && !Array.isArray(p) ? (p as Record<string, unknown>) : null;
                  return (
                    <tr key={i} className="border-t" data-testid="policy">
                      <td className="py-1 pr-3">{o !== null && typeof o.name === "string" ? o.name : rawText(o === null ? p : o.name)}</td>
                      <td className="py-1 pr-3 font-mono text-[12px]">{o !== null && typeof o.command === "string" ? o.command : "unknown"}</td>
                      <td className="py-1 font-mono text-[12px]">{o !== null && typeof o.table === "string" ? o.table : "unknown"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {!complete && <p className="m-0 text-[12.5px] text-muted-foreground">The policies the migrations create; the table is not complete, so others may exist.</p>}
          </>
        )
      ) : (
        <p className="m-0 text-[13px] text-muted-foreground italic" data-testid="policies-unknown">
          Policies unknown: a statement the pass does not read may have created, renamed or dropped one.
        </p>
      )}
    </section>
  );
}
