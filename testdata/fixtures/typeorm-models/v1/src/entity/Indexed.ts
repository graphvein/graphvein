import { Column, Entity, Index, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Indexed {
  @PrimaryGeneratedColumn()
  id: number;

  @Index({ type: "hash" })
  @Column()
  code: string;
}
