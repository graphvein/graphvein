import { PrismaClient } from "@prisma/client";
import { prisma } from "./db";

// CT-R1: every operation, through a Nest-style injected client, the
// imported module object and a transaction callback.
export class UserService {
  constructor(private readonly prisma: PrismaClient) {}

  async reads(id: number) {
    await this.prisma.user.findUnique({ where: { id } });
    await this.prisma.user.findUniqueOrThrow({ where: { id } });
    await this.prisma.post.findFirst({ where: { id } });
    await this.prisma.post.findFirstOrThrow({ where: { id } });
    await this.prisma.tag.findMany();
    await this.prisma.comment.count();
    await this.prisma.post.aggregate({ _count: true });
    await this.prisma.post.groupBy({ by: ["authorId"] });
  }

  async writes(id: number) {
    await this.prisma.user.create({ data: { email: "a@example.com" } });
    await this.prisma.tag.createMany({ data: [{ name: "a" }] });
    await this.prisma.tag.createManyAndReturn({ data: [{ name: "b" }] });
    await this.prisma.user.update({ where: { id }, data: { email: "b@example.com" } });
    await this.prisma.tag.updateMany({ where: { name: "a" }, data: { name: "c" } });
    await this.prisma.tag.updateManyAndReturn({ where: { name: "c" }, data: { name: "d" } });
    await this.prisma.tag.upsert({ where: { name: "d" }, create: { name: "d" }, update: {} });
    await this.prisma.comment.delete({ where: { id } });
    await this.prisma.comment.deleteMany({});
  }
}

export async function moduleClient() {
  return prisma.profile.findMany({});
}

export async function inTransaction() {
  await prisma.$transaction(async (tx) => {
    await tx.post.create({ data: { title: "t", authorId: 1 } });
  });
  await prisma.$transaction([prisma.tag.deleteMany({}), prisma.tag.count()]);
}

// Row D: a transaction over queries built elsewhere.
export async function notTraced(queries: any[]) {
  await prisma.$transaction(queries);
}

// CT-R1 refusal: a dynamic delegate (umami's pagedQuery helper form).
export async function dynamic(client: any, model: string) {
  return client[model].findMany({});
}

// No sites: an @@ignore'd model has no delegate, a name that is no
// model, an operation outside the lists, a receiver with no delegate.
export async function notSites(cache: Map<string, number>) {
  await prisma.legacy.findMany();
  await prisma.widget.findMany();
  await prisma.user.findBySlug("x");
  cache.delete("x");
}

// A module-level site: its owner is the variable.
export const allTags = prisma.tag.findMany();
