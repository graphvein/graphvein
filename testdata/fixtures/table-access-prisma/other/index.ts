// A package without a Prisma group: a delegate-shaped call is no site (W7).
export async function elsewhere(db: any) {
  return db.user.findMany();
}
