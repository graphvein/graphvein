import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { BetaModule } from "./features";

// Revision 4 A4: root B reaches beta.module.ts through a re-export.
@Module({
  imports: [TypeOrmModule.forRoot({ type: "mysql", database: "b", autoLoadEntities: true }), BetaModule],
})
export class AppBModule {}
