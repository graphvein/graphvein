import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Gamma {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  label: string;
}
