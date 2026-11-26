function makeHandlers() {
  return { GET: async () => Response.json({}) };
}

export const { GET } = makeHandlers();
