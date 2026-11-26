export async function load(id: string, slug: string, name: string, file: string, part: string) {
  await fetch(`/t035-next/items/${id}`);
  await fetch(`/t035-next/docs/${slug}`);
  await fetch(`/t035-go/files/${name}`);
  await fetch(`/t035-axum/assets/${file}`);
  await fetch(`/t035-py/api/${part}`);
  await fetch('/t035-py/api/v1/flows');
  await fetch(`https://cdn.example.org/t035-py/api/${part}/meta`);
  await fetch('/t035-next/items/featured');
}
