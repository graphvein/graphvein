# Angular template matching

Explicit standalone values keep scope expectations independent of version
defaults. targets.ts supplies selectors and pipe names; hosts.ts and modules.ts
exercise local, open, unknown, invalid and absent compilation scopes. Two hosts
share shared.html. attributes.html preserves raw static and binding spellings.
pipes.html includes invalid action/track contexts and expression recovery.
recovery.ts covers structural recovery and connected/orphan blocks, using inline
host coordinates. DotPath and RootedPath are unresolved resource cases.

No ignore-file or ignored-directory mappings are needed.
