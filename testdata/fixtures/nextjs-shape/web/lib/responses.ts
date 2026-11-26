// Response helpers the handlers return (row SH-N25).
export function badRequest(message: string) {
  return Response.json({ error: { message, code: 'bad_request' } }, { status: 400 });
}

export function created(value: unknown) {
  return Response.json(value, { status: 201 });
}

export function reply(value: unknown, status: number) {
  return Response.json(value, { status });
}

export function accepted() {
  return Response.json({ accepted: true }, { status: 202 });
}
