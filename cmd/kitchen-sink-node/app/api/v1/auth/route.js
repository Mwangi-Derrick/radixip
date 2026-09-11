import { NextResponse } from 'next/server';

export async function GET() {
  return NextResponse.json({ framework: 'nextjs', route: 'auth-get' });
}

export async function POST() {
  return NextResponse.json({ framework: 'nextjs', route: 'auth-post' });
}
