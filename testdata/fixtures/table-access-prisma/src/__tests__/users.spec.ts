import { prisma } from "../db";

// A test-scope site, and a mock call that is no site.
export async function check(prismaMock: any) {
  prismaMock.user.findMany.mockResolvedValue([]);
  return prisma.user.findMany({ include: { posts: true } });
}
