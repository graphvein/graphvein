# ts-bridges fixture (T026)

Cases of the bridge claim contract `docs/lang/typescript-bridges.md`: JSX
renders (JR1-JR3), passed functions (PF1-PF3), react-router route objects
(RR1-RR5) and the default branch D. `TestTypeScriptBridgesFixture` keeps
the projection in `../ts-bridges.golden.json`; `TestTypeScriptBridgesContract`
checks every row and D are reached. No inert names are needed: the
fixture holds no ignore files or ignored paths.
