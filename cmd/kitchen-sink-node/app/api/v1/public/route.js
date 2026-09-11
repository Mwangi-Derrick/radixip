import { NextResponse } from 'next/server';

export async function GET() {
  return NextResponse.json({ framework: 'nextjs', route: 'public' });
}
