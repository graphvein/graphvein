import { Column, Entity, PrimaryGeneratedColumn, RelationCount } from "typeorm";

@Entity()
export class Counted {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  name: string;

  @RelationCount((c: Counted) => c.name)
  total: number;
}
