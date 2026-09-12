import { NextResponse } from 'next/server';
import { enforceRadixIP } from '../_radixip';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function GET(request) {
  const denied = enforceRadixIP(request);
  if (denied) return denied;
  return NextResponse.json({ framework: 'nextjs', route: 'auth-get' });
}

export async function POST(request) {
  const denied = enforceRadixIP(request);
  if (denied) return denied;
  return NextResponse.json({ framework: 'nextjs', route: 'auth-post' });
}
