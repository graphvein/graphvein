import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Alpha {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  label: string;
}
