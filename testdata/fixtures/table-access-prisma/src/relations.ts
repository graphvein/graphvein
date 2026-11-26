import { prisma } from "./db";

// CT-R2: relation fields under include, select and relation filters,
// nested at any depth.
export async function reads(id: number) {
  await prisma.user.findMany({
    include: { posts: { include: { comments: true }, where: { title: "x" } }, profile: true },
  });
  await prisma.post.findMany({ select: { id: true, author: { select: { email: true } }, comments: false } });
  await prisma.user.findMany({ where: { posts: { some: { comments: { every: { body: "b" } } } } } });
  await prisma.user.count({ where: { comments: { none: {} } } });
  await prisma.post.findFirst({ where: { author: { is: { email: "e" } }, AND: [{ author: { isNot: { id } } }] } });
  await prisma.user.findMany({ where: { email: "x" } });
}

// CT-R2: nested writes under data.
export async function writes(id: number) {
  await prisma.user.create({
    data: {
      email: "n",
      posts: { create: [{ title: "p", comments: { createMany: { data: [{ body: "c", authorId: 1 }] } } }] },
      profile: { connectOrCreate: { where: { userId: id }, create: { bio: "b" } } },
    },
  });
  await prisma.user.update({
    where: { id },
    data: {
      posts: {
        update: { where: { id }, data: { title: "u" } },
        updateMany: { where: { title: "u" }, data: { title: "v" } },
        deleteMany: { title: "v" },
      },
      profile: { upsert: { create: { bio: "c" }, update: { bio: "d" } } },
      comments: { delete: { id } },
    },
  });
  await prisma.post.update({ where: { id }, data: { author: { update: { email: "z" } } } });
}

// CT-R2 refusals and partially read sites (Revision 3).
export async function refusals(id: number, criteria: object, args: any, include: any) {
  await prisma.user.findMany({ ...criteria, include: { posts: true } });
  await prisma.post.update({ where: { id }, data: { author: { connect: { id } } } });
  await prisma.post.update({ where: { id }, data: { tags: { connect: [{ id }], disconnect: [{ id }] } } });
  await prisma.user.update({ where: { id }, data: { groups: { set: [] } } });
  await prisma.post.findMany({ include: { tags: true } });
  await prisma.post.findMany({ orderBy: { author: { email: "asc" } } });
  await prisma.post.findMany({ where: { author: { email: "e" } } });
  await prisma.post.findMany({ where: { author: { is: null } } });
  await prisma.user.findMany({ include: { _count: { select: { posts: true } } } });
  await prisma.user.findMany(args);
  await prisma.user.findMany({ include });
}

// R5-C1: the fluent API reads the related table, a form the rows do not
// claim: site_partial with the related table (and an implicit many-to-many
// join table) as candidates; the delegate call is its own CT-R1 site.
export async function fluent(id: number) {
  await prisma.user.findUnique({ where: { id } }).posts();
  await prisma.post.findFirst({ where: { id } }).author().profile();
  await prisma.user.findUniqueOrThrow({ where: { id } }).groups();
  await prisma.user.findUnique({ where: { id } }).then((u) => u);
}
