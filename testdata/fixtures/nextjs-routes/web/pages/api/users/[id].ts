function withAuth<T>(inner: T): T {
  return inner;
}

export default withAuth((req: unknown, res: { end(): void }) => res.end());
