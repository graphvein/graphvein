import express from "express";
const app = express();
function one() {}
function two() {}
app.get("/t013b/relative", one);
app.get("/t013b/absolute", one);
app.get("/t013b/repaired", one);
app.get("/t013b/overridden", one);
app.get("/t013b/param/:id", one);
app.get("/t013b/literal/fixed", one);
app.get("/t013b/specific/fixed", one);
app.get("/t013b/specific/:id", two);
app.get("/t013b/head", one);
app.all("/t013b/any", one);
app.get("/t013b/get-only", one);
app.get("/t013b/duplicate", one);
app.get("/t013b/duplicate", one);
app.get("/t013b/tie", one);
app.get("/t013b/tie", two);
app.get("/t013b/a/multi", one);
app.get("/t013b/b/multi", two);
app.get("/t013b/wild/*", one);
app.get("/t013b/optional/:id?", one);
