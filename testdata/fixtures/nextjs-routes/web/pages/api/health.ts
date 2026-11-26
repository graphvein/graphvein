export default function handler(req: unknown, res: { status(code: number): { end(): void } }) {
  res.status(200).end();
}
