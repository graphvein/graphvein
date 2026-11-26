export async function POST(request: Request) {
  const form = await request.formData();
  const email = form.get('email');
  const files = form.getAll('attachment');
  return new Response(JSON.stringify({ email, files: files.length }), {
    status: 201,
    headers: { 'content-type': 'application/json' },
  });
}
