import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (3) no annotation: design type Object.
@Entity()
export class NoAnnotation {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value;
}
