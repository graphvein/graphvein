import express, { Request } from 'express';
import { NewPet } from './schemas';
import { Pet, PetInput } from './types';

const app = express();
app.use(express.json());

async function loadPet(id: number): Promise<Pet> {
  return { id, name: 'x' };
}

app.post('/pets', (req: Request<{}, any, PetInput>, res) => {
  res.status(201).json({ name: req.body.name });
});

app.post('/pets/checked', (req, res) => {
  const body = NewPet.parse(req.body);
  res.status(201).json({ name: body.name });
});

app.get('/pets/:id', async (req, res) => {
  const pet = await loadPet(Number(req.params.id));
  res.json(pet);
});

app.listen(3000);
