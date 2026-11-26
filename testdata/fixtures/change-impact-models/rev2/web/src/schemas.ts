import { z } from 'zod';

export const NewPet = z.object({
  name: z.string().min(1),
  tag: z.string().optional(),
});
