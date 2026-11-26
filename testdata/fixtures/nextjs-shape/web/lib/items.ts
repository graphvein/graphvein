export interface Item {
  id: string;
  name: string;
  price: number;
  tags?: string[];
}

export async function findItem(id: string): Promise<Item | null> {
  if (id === 'none') {
    return null;
  }
  return { id, name: 'Lamp', price: 12 };
}
