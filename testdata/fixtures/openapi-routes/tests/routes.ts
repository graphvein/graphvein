import express from "express";
const app = express();
function testOnly() {}
app.get("/t013b/only-test", testOnly);
app.get("/t013b/test-spec", testOnly);
