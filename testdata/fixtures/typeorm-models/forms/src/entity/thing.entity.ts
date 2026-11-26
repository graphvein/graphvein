import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Thing {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ length: 30 })
  label: string;
}
