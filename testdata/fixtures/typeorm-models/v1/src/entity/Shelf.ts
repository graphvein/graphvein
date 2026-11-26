import { Check, Column, Entity, Index, PrimaryColumn } from "typeorm";

// Under 1.x, generated names hash with standard SHA-1, so a
// partial index and a check expression get other names than on
// 0.3.x; a simple-enum column's CHECK on sqlite (TO-C5).
@Entity()
@Check(`"size" > 0`)
export class Shelf {
  @PrimaryColumn()
  code: string;

  @PrimaryColumn()
  region: string;

  @Column()
  size: number;

  @Index({ where: `"size" > 1` })
  @Column()
  rank: number;

  @Column({ type: "simple-enum", enum: ["oak", "pine"] })
  wood: string;
}
