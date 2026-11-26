import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@/index.css";
import { normaliseHash } from "@/router";
import { AppShell, FirstRunPage, ShellPlaceholder } from "@/shell/AppShell";
import { ProjectRoot } from "@/shell/project";
import { AddFolderDialog } from "@/shell/workspace/AddFolder";
import { WorkspaceRoot } from "@/shell/workspace/WorkspaceRoot";

normaliseHash();

const root = document.getElementById("root");
if (root === null) throw new Error("index.html has no #root element");
createRoot(root).render(
  <StrictMode>
    {/* The workspace (T038) names the viewed index; the page below it
        remounts when that changes. Under a project the shell remounts when
        the repository filter changes too (T022 B9); a single repository
        renders it as before. */}
    <WorkspaceRoot
      shell={(entry) => (
        <ProjectRoot
          key={entry?.id ?? ""}
          single={entry !== null && entry.kind === "repository"}
          {...(entry !== null ? { placeholder: <ShellPlaceholder /> } : {})}
          shell={(repository) => <AppShell key={repository ?? ""} />}
        />
      )}
      empty={() => <FirstRunPage />}
      overlay={<AddFolderDialog />}
    />
  </StrictMode>,
);
