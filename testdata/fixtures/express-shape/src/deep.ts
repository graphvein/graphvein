// A chain of eight calls that pass the body and the response on: the
// default depth (6) stops it with depth_cap.
export function level1(body: any, res: any) {
  const a = body.a;
  level2(body, res);
}
function level2(body: any, res: any) {
  const b = body.b;
  level3(body, res);
}
function level3(body: any, res: any) {
  level4(body, res);
}
function level4(body: any, res: any) {
  level5(body, res);
}
function level5(body: any, res: any) {
  level6(body, res);
}
function level6(body: any, res: any) {
  level7(body, res);
}
function level7(body: any, res: any) {
  level8(body, res);
}
function level8(body: any, res: any) {
  res.json({ deepest: body.h });
}
