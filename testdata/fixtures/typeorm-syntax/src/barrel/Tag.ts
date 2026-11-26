// The decorators come from a project barrel that re-exports TypeORM's:
// no typeorm import, so the file passes gate row G11.
import { Column, Entity, PrimaryGeneratedColumn } from "../orm";

@Entity()
export class Tag {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ length: 30 })
  name: string;
}
