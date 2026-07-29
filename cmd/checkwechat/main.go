package main

import (
	"fmt"
	"os"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/payment/wechatv3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== 微信支付配置诊断 ===")
	fmt.Printf("商户号 WECHAT_MCH_ID: %s\n", cfg.Payment.WeChatMchID)
	fmt.Printf("小程序 AppID: %s\n", cfg.WeChat.AppID)
	fmt.Printf("APIv3 密钥长度: %d（应为 32）\n", len(cfg.Payment.WeChatAPIKey))
	fmt.Printf("证书序列号: %s\n", cfg.Payment.WeChatSerialNo)
	fmt.Printf("私钥路径: %s\n", cfg.Payment.WeChatKeyPath)
	fmt.Printf("证书路径: %s\n", cfg.Payment.WeChatCertPath)
	fmt.Printf("回调地址: %s\n", cfg.Payment.WeChatNotifyURL)
	fmt.Println()

	wcCfg := wechatv3.Config{
		MchID:    cfg.Payment.WeChatMchID,
		SerialNo: cfg.Payment.WeChatSerialNo,
		CertPath: cfg.Payment.WeChatCertPath,
		KeyPath:  cfg.Payment.WeChatKeyPath,
		APIKey:   cfg.Payment.WeChatAPIKey,
	}
	client, err := wechatv3.NewClient(wcCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 客户端初始化失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ 商户私钥与证书序列号可用（V3 签名正常）")

	if certSerial, err := wechatv3.ReadCertSerialNo(cfg.Payment.WeChatCertPath); err != nil {
		fmt.Printf("⚠ 无法读取证书序列号: %v\n", err)
	} else if certSerial != cfg.Payment.WeChatSerialNo {
		fmt.Printf("❌ 证书序列号不匹配: .env=%s, 证书文件=%s\n", cfg.Payment.WeChatSerialNo, certSerial)
		fmt.Printf("   请将 WECHAT_PAY_SERIAL_NO 改为: %s\n", certSerial)
	} else {
		fmt.Println("✓ 证书序列号与 apiclient_cert.pem 一致")
	}

	fmt.Println()
	fmt.Println("正在验证 APIv3 密钥（解密平台证书）...")
	if err := client.VerifyAPIKey(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		fmt.Println()
		fmt.Println("修复步骤:")
		fmt.Println("  1. 登录 https://pay.weixin.qq.com → 账户中心 → API安全")
		fmt.Println("  2. 找到「APIv3密钥」（不是 APIv2 密钥 / 商户API密钥）")
		fmt.Println("  3. 若不确定当前密钥，可「设置APIv3密钥」重新生成 32 位密钥")
		fmt.Println("  4. 将新密钥写入 backend/.env 的 WECHAT_PAY_API_KEY（不要加引号、不要行内注释）")
		fmt.Println("  5. 重启服务后再次运行: go run ./cmd/checkwechat/")
		os.Exit(1)
	}

	fmt.Println("✓ APIv3 密钥验证通过")
	if err := client.FetchPlatformCerts(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ 平台证书缓存失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ 平台证书下载并解密成功，支付回调验签可用")
	fmt.Println()
	fmt.Println("微信支付配置正常，可以进行 JSAPI 下单与回调测试。")
}
