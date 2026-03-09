import React from "react";
import ReactDOM from "react-dom/client";
import { App as AntdApp, ConfigProvider } from "antd";

import { AppRoot } from "./app/App";
import "./index.css";
import "antd/dist/reset.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      theme={{
        token: {
          colorPrimary: "#1447e6",
          borderRadius: 12,
          fontFamily: "'PingFang SC', 'Microsoft YaHei', 'Segoe UI', sans-serif",
        },
      }}
    >
      <AntdApp>
        <AppRoot />
      </AntdApp>
    </ConfigProvider>
  </React.StrictMode>,
);
