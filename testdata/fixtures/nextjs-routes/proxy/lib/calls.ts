// Calls under the literal rewrites of next.config.js (row NX-L3). Sources
// and destinations take the base path /proxy, as Next.js adds it.
export async function calls(seg: string) {
  const id = String(Date.now());
  await fetch('/proxy/old/a/b'); // beforeFiles: served by /proxy/api/new/a/b
  await fetch('/proxy/ext/x'); // external destination: withheld
  await fetch('/proxy/odd/1'); // destination not of whole segments: withheld
  await fetch('/proxy/hop/1'); // a later afterFiles rule may rewrite again: withheld
  await fetch(`/proxy/${seg}/a/b`); // may match /old/:path*, not provably: withheld
  await fetch('/proxy/OLD/a'); // matches only case-insensitively: withheld
  await fetch('/proxy/api/static'); // a static route wins over afterFiles
  await fetch('/proxy/api/items/7'); // afterFiles beats the dynamic route
  await fetch(`/proxy/api/items/${id}`); // the call parameter is carried over
  await fetch('/proxy/api/real'); // a route wins over fallback
  await fetch('/proxy/api/missing'); // fallback: served by /proxy/api/fallback/missing
  await fetch('/old/a/b'); // without the base path no rule matches
}
