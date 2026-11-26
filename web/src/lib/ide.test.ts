import { describe, expect, it } from "vitest";

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import { presentIde } from "@/lib/ide";
import { editorState } from "@/test/ideFixtures";

const ZED: IdeLink = { link: "zed://file/r/a.ts:2:19", line: 2, column: 19, exact: true, line_only: false, caveat: null };
const VS: IdeLink = { link: "vscode://file/r/a.ts:2:20", line: 2, column: 20, exact: true, line_only: false, caveat: null };

function status(o: Partial<IdeStatus>): IdeStatus {
  return { available: true, error: null, label: null, editor: null, source: null, ...o };
}

describe("presentIde", () => {
  it("names the editor and source from the response in all three precedence cases", () => {
    const request = presentIde(status({ label: "Open in Zed", editor: "zed", source: "request" }), ZED, editorState({ choice: "zed" }));
    expect(request.text).toBe("Open in Zed");
    expect(request.name).toBe("Open in Zed, your choice in this browser");
    expect(request.title).toBe("zed://file/r/a.ts:2:19\nZed, your choice in this browser");
    expect(request.caveat).toBeNull();
    const repo = presentIde(status({ label: "Open in VS Code", editor: "vscode", source: "repository" }), VS, editorState());
    expect(repo.title).toBe("vscode://file/r/a.ts:2:20\nVS Code, the repository default from graphvein.yaml");
    const builtin = presentIde(status({ label: "Open in VS Code", editor: "vscode", source: "builtin" }), VS, editorState());
    expect(builtin.title).toBe("vscode://file/r/a.ts:2:20\nVS Code, Graphvein's default (graphvein.yaml sets no editor)");
    expect(builtin.mark).toEqual({ kind: "preset", editor: "vscode" });
    const custom = presentIde(status({ label: null, editor: null, source: "repository" }), { ...VS, link: "subl://open?url=a" }, editorState());
    expect(custom.title).toBe("subl://open?url=a\ncustom link from graphvein.yaml, not verified by Graphvein");
    expect(custom.caveat).toBe("warn");
  });

  it("carries the caveat of a preset that is not verified on the button", () => {
    const cursor = presentIde(status({ label: "Open in Cursor", editor: "cursor", source: "request" }), { ...VS, link: "cursor://file/r/a.ts:2:20" }, editorState({ choice: "cursor" }));
    expect(cursor.caveat).toBe("warn");
    expect(cursor.name).toMatch(/^Open in Cursor, your choice in this browser \(partly verified: cursor:\/\/file is confirmed/);
    const idea = presentIde(status({ label: "Open in IntelliJ IDEA", editor: "jetbrains", source: "request" }), VS, editorState({ choice: "jetbrains" }));
    expect(idea.caveat).toBe("info");
    expect(idea.name).toMatch(/^Open in IntelliJ IDEA, your choice in this browser \(limited: macOS only, IntelliJ IDEA only; where a tab precedes the node/);
    expect(idea.name).not.toContain("opens the line only:");
  });

  it("keeps the changed-file suffix", () => {
    const v = presentIde(status({ label: "Open in Zed", editor: "zed", source: "request" }), { ...ZED, exact: false }, editorState({ choice: "zed" }));
    expect(v.title).toBe("zed://file/r/a.ts:2:19 (the file changed; the line may have moved)\nZed, your choice in this browser");
  });

  it("says a line-only link after a tab opens the line, never that the file changed", () => {
    const line: IdeLink = { link: "idea://open?file=/r/a.go&line=4", line: 4, column: 0, exact: false, line_only: true, caveat: "tab_size_unknown" };
    const v = presentIde(status({ label: "Open in IntelliJ IDEA", editor: "jetbrains", source: "request" }), line, editorState({ choice: "jetbrains" }));
    expect(v.href).toBe("idea://open?file=/r/a.go&line=4");
    expect(v.title).not.toContain("the line may have moved");
    expect(v.name).toMatch(/^Open in IntelliJ IDEA, your choice in this browser \(limited: macOS only, IntelliJ IDEA only; .*; opens the line only: a tab precedes the node and the editor's tab size is not known\)$/);
    expect(v.caveat).toBe("info");
    // Without a preset caveat (a repository template equal to jetbrains') the icon still shows.
    const plain = presentIde(status({ label: null, editor: null, source: "repository" }), line, editorState());
    expect(plain.caveat).toBe("warn");
    const vs = presentIde(status({ label: "Open in VS Code", editor: "vscode", source: "builtin" }), { ...VS, line_only: true, exact: false }, editorState());
    expect(vs.caveat).toBe("info");
    expect(vs.name).toContain("opens the line only");
  });

  it("says a line-only link on a changed file opens the line, which may have moved", () => {
    const line: IdeLink = { link: "idea://open?file=/r/a.go&line=4", line: 4, column: 0, exact: false, line_only: true, caveat: null };
    const v = presentIde(status({ label: "Open in IntelliJ IDEA", editor: "jetbrains", source: "request" }), line, editorState({ choice: "jetbrains" }));
    expect(v.title).toMatch(/^idea:\/\/open\?file=\/r\/a\.go&line=4\n/);
    expect(v.name).toMatch(/; opens the line only: the file changed since indexing; the line may have moved\)$/);
    expect(v.name).not.toContain("a tab precedes the node and");
  });

  it("says a Cursor column unit is assumed, not that the file changed", () => {
    const link: IdeLink = { ...VS, link: "cursor://file/r/a.ts:2:20", exact: false, caveat: "column_unit_assumed" };
    const v = presentIde(status({ label: "Open in Cursor", editor: "cursor", source: "request" }), link, editorState({ choice: "cursor" }));
    expect(v.title).not.toContain("the file changed");
    expect(v.name).toMatch(/; column unit assumed from VS Code\)$/);
    expect(v.caveat).toBe("warn");
    const unknown = presentIde(status({ label: "Open in Zed", editor: "zed", source: "request" }), { ...ZED, exact: false, caveat: "new_reason" }, editorState({ choice: "zed" }));
    expect(unknown.name).toMatch(/\(the column may be off\)$/);
    expect(unknown.caveat).toBe("info");
  });

  it("names the editor the response built and notes an ignored choice", () => {
    // An older server ignores editor= and answers with its own editor.
    const v = presentIde(status({ label: "Open in VS Code" }), VS, editorState({ choice: "zed" }));
    expect(v.text).toBe("Open in VS Code");
    expect(v.name).toBe("Open in VS Code (This server ignored your editor choice)");
    expect(v.caveat).toBe("warn");
    expect(v.name).not.toContain("Zed");
  });

  it("disables every button with the reason when links are off or unavailable", () => {
    const off = presentIde(status({ label: "Open in VS Code", editor: "vscode", source: "builtin" }), VS, editorState({ choice: "off" }));
    expect(off.href).toBeNull();
    expect(off.name).toBe("Editor links are off in this browser (your choice)");
    const none = presentIde(status({ available: false, error: "bad yaml", source: "repository" }), null, editorState());
    expect(none.href).toBeNull();
    expect(none.title).toBe("IDE links are unavailable: bad yaml");
  });
});
