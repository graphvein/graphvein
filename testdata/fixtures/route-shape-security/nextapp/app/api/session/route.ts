// A route handler that reads the next-auth session (row SH-S23).
import { getServerSession } from "next-auth";
import { NextResponse } from "next/server";
import { authOptions } from "../../../lib/auth";

export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }
  return NextResponse.json({ ok: true });
}
