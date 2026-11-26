export function HEAD() {
  return new Response(null);
}

export function OPTIONS() {
  return new Response(null, { headers: { Allow: 'HEAD, OPTIONS' } });
}
