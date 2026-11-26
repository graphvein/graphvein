export async function search() {
  await fetch(`${process.env.NEXT_PUBLIC_BASE_PATH}/api/search`);
  await fetch('/api/search');
}
