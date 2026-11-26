export async function showPet(): Promise<unknown> {
  const res = await fetch('/pets/1');
  return res.json();
}
