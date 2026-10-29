const { createRequire } = require("node:module");
const { join } = require("node:path");

const source = process.env.AGENTTRACE_NATIVE_LANGFUSE_SOURCE;
const requireShared = createRequire(join(source, "packages/shared/package.json"));
const { prisma } = requireShared("./dist/src/db.js");
const { createAndAddApiKeysToDb } = requireShared("./dist/src/server/auth/apiKeys.js");
const { invalidateCachedApiKeys } = requireShared("./dist/src/server/auth/invalidateApiKeys.js");
const { hash } = requireShared("bcryptjs");

async function provision() {
  const e = process.env;
  // Mirrors web/src/initialize.ts for the dedicated local database. The installed
  // production build has its NEXT_PUBLIC_LANGFUSE_RUN_NEXT_INIT branch removed.
  const org = await prisma.organization.upsert({
    where: { id: e.LANGFUSE_INIT_ORG_ID }, update: {},
    create: { id: e.LANGFUSE_INIT_ORG_ID, name: e.LANGFUSE_INIT_ORG_NAME },
  });
  await prisma.project.upsert({
    where: { id: e.LANGFUSE_INIT_PROJECT_ID }, update: {},
    create: { id: e.LANGFUSE_INIT_PROJECT_ID, name: e.LANGFUSE_INIT_PROJECT_NAME, orgId: org.id },
  });
  let user = await prisma.user.findUnique({ where: { email: e.LANGFUSE_INIT_USER_EMAIL } });
  if (!user) user = await prisma.user.create({
    data: {
      email: e.LANGFUSE_INIT_USER_EMAIL, name: e.LANGFUSE_INIT_USER_NAME,
      password: await hash(e.LANGFUSE_INIT_USER_PASSWORD, 12),
    },
  });
  await prisma.organizationMembership.upsert({
    where: { orgId_userId: { userId: user.id, orgId: org.id } },
    update: { role: "OWNER" }, create: { userId: user.id, orgId: org.id, role: "OWNER" },
  });
  let key = await prisma.apiKey.findUnique({ where: { publicKey: e.LANGFUSE_INIT_PROJECT_PUBLIC_KEY } });
  if (key && key.projectId !== e.LANGFUSE_INIT_PROJECT_ID) throw new Error("Local API key belongs to another project.");
  if (!key) {
    await createAndAddApiKeysToDb({
      prisma, entityId: e.LANGFUSE_INIT_PROJECT_ID, scope: "PROJECT", note: "Native local headless provisioning",
      predefinedKeys: { publicKey: e.LANGFUSE_INIT_PROJECT_PUBLIC_KEY, secretKey: e.LANGFUSE_INIT_PROJECT_SECRET_KEY },
    });
    key = await prisma.apiKey.findUnique({ where: { publicKey: e.LANGFUSE_INIT_PROJECT_PUBLIC_KEY } });
  }
  await invalidateCachedApiKeys([key], "native local project");
}

provision().then(async () => {
  await prisma.$disconnect();
  process.exit(0);
}).catch(async () => {
  await prisma.$disconnect();
  console.error("Native headless provisioning failed; inspect the dedicated database and runtime configuration.");
  process.exit(1);
});
