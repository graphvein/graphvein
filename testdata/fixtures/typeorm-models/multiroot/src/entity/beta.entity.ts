import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Beta {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  label: string;
}
