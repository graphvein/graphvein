Export of task @TASK@ from the maintainer's working repository: one
squashed commit whose tree is the public snapshot of the maintainer's main
branch after this task.

@NOTES@

Before the export, the snapshot passed the public tree check
(`scripts/check-public-tree.sh`) and the privacy check
(`scripts/check-public-privacy.sh`). This pull request runs the full CI
matrix. After the squash-merge, the maintainer compares the merged tree with
the snapshot (`scripts/export-public.sh --check-merged`).
