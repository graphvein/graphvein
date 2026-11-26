import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { AlphaModule } from "./alpha.module";

// Revision 4 A4: root A reaches only the forFeature calls of the files it
// imports (alpha.module.ts), not root B's.
@Module({
  imports: [TypeOrmModule.forRoot({ type: "mysql", database: "a", autoLoadEntities: true }), AlphaModule],
})
export class AppAModule {}
