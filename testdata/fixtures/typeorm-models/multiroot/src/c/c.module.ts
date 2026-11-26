import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";

// Revision 4 A4: no static import reaches gamma.module.ts (a lazily loaded
// module), so root C's table list may be incomplete.
@Module({
  imports: [TypeOrmModule.forRoot({ name: "lazy", type: "mysql", database: "c", autoLoadEntities: true })],
})
export class AppCModule {}
