import { Module } from "@nestjs/common";
import { ConfigService } from "@nestjs/config";
import { TypeOrmModule } from "@nestjs/typeorm";
import { DataSource } from "typeorm";
import { UserModule } from "./user/user.module";

@Module({
  imports: [
    TypeOrmModule.forRootAsync({
      inject: [ConfigService],
      useFactory: (config: ConfigService) => {
        let logging = process.env.DB_LOGGING;
        try {
          logging = JSON.parse(logging ?? "false");
        } catch {
          // ignore
        }
        return { ...config.get("database"), autoLoadEntities: true, logging };
      },
      dataSourceFactory: async (options) => {
        const dataSource = await new DataSource(options).initialize();
        return dataSource;
      },
    }),
    UserModule,
  ],
})
export class AppModule {}
