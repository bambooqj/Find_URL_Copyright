package main

import (
  "bufio"
  "context"
  "encoding/csv"
  "flag"
  "fmt"
  "github.com/chromedp/chromedp"
  "log"
  "os"
  "regexp"
  "strings"
  "sync"
  "time"
)

type ExtractedData struct {
  URL         string
  ICP         string
  TechSupport string
  Copyright   string
}

// goreleaser --snapshot --skip-publish --rm-dist
func main() {
  // 从命令行参数获取文件名
  var fileName string
  flag.StringVar(&fileName, "file", "urls.txt", "file containing URLs to process")
  flag.Parse()

  // 从文件中读取网址
  file, err := os.Open(fileName)
  if err != nil {
    log.Fatal(err)
  }
  defer file.Close()

  // 使用带缓冲的读取器读取文件
  scanner := bufio.NewScanner(file)

  // 使用WaitGroup和信号量限制并发数
  wg := &sync.WaitGroup{}
  semaphore := make(chan struct{}, 5)

  // 创建 CSV 输出文件
  outputFile, err := os.Create("output.csv")
  if err != nil {
    log.Fatal(err)
  }
  defer outputFile.Close()

  // 初始化 CSV Writer
  csvWriter := csv.NewWriter(outputFile)
  defer csvWriter.Flush()

  // 写入 CSV 表头
  err = csvWriter.Write([]string{"URL", "ICP", "TechSupport", "Copyright"})
  if err != nil {
    log.Fatal(err)
  }

  // 按行读取文件
  for scanner.Scan() {
    url := scanner.Text()

    wg.Add(1)
    go func(url string) {
      defer wg.Done()

      semaphore <- struct{}{}
      defer func() { <-semaphore }()

      extractedData := extractData(url)

      err := csvWriter.Write([]string{extractedData.URL, extractedData.ICP, extractedData.TechSupport, extractedData.Copyright})
      if err != nil {
        log.Println("Error writing to CSV:", err)
      }

      fmt.Printf("Extracted data: %+v\n", extractedData)
    }(url)
  }

  if err := scanner.Err(); err != nil {
    log.Fatal(err)
  }

  wg.Wait()
}

func extractData(url string) ExtractedData {
  // 创建浏览器上下文
  ctx, cancel := chromedp.NewContext(context.Background(), chromedp.WithLogf(log.Printf))
  defer cancel()

  // 设置超时时间
  ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
  defer cancel()

  // 提取网页底部数据
  var data string
  err := chromedp.Run(ctx,
    chromedp.Navigate(url),
    chromedp.WaitVisible(`body`, chromedp.ByQuery),
    chromedp.Evaluate(`
function findFooterElement() {
    const visibleElements = Array.from(document.querySelectorAll('div'))
        .filter(element => getComputedStyle(element).display !== 'none' && getComputedStyle(element).visibility !== 'hidden');
    let lastVisibleElement = visibleElements.find(element => {
        const text = element.textContent;
        const hasFootInIdOrClass = element.id.includes('foot') || element.className.includes('foot');
        const isValidFooterContent = /(?:\w|[\u4e00-\u9fa5])*ICP(备)?\d{1,10}(号)(-\d{1,})?/.test(text) ||
            /(技术支持|技术提供|网站建设|技术服务)[:：\s]*([A-Za-z0-9\u4e00-\u9fa5\(\)-]+)/.test(text) ||
            /(?:.*?)(?:copyright|©|版权).+?([\u4e00-\u9fa5]+(?:（[^）]+）)?)/i.test(text);

        return isValidFooterContent && (text.length <= 300 || hasFootInIdOrClass);
    });

    // 如果未找到带有 foot 的元素，全局检测正则
    if (!lastVisibleElement) {
        lastVisibleElement = visibleElements.find(element => {
            const text = element.textContent;
            const isValidFooterContent = /(?:\w|[\u4e00-\u9fa5])*ICP(备)?\d{1,10}(号)(-\d{1,})?/.test(text) ||
                /(技术支持|技术提供|网站建设|技术服务)[:：\s]*([A-Za-z0-9\u4e00-\u9fa5\(\)-]+)/.test(text) ||
                /(?:.*?)(?:copyright|©|版权).+?([\u4e00-\u9fa5]+(?:（[^）]+）)?)/i.test(text);

            return isValidFooterContent;
        });
    }

    return lastVisibleElement;
}
const footerElement = findFooterElement();
footerElement ? footerElement.textContent : '';
    `, &data),
  )
  if err != nil {
    log.Println("Error:", err)
    return ExtractedData{URL: url}
  }

  icpRegex := regexp.MustCompile(`(?:\w|\p{Han})*ICP(备)?\d{1,10}(号)(-\d{1,})?`)
  techSupportRegex := regexp.MustCompile(`(?m)(技术支持|技术提供|网站建设|技术服务)[:：\s]*([A-Za-z0-9\x{4e00}-\x{9fa5}\(\)-]+)`)
  copyrightRegex := regexp.MustCompile(`(?i)(?:.*?)(?:copyright|©|版权).+?([\x{4e00}-\x{9fa5}]+)(?:.+?©.+?[\x{4e00}-\x{9fa5}]+)?`)

  icp := icpRegex.FindString(data)
  icp = mergeSpaces(icp)
  if len([]rune(icp)) < 10 || len([]rune(icp)) > 40 {
    icp = ""
  }
  data = strings.Replace(data, icp, "", -1)
  data = strings.Replace(data, "版权声明", "", -1)

  techSupport := techSupportRegex.FindString(data)
  techSupport = mergeSpaces(techSupport)
  if len([]rune(techSupport)) < 10 || len([]rune(techSupport)) > 40 {
    techSupport = ""
  }
  data = strings.Replace(data, techSupport, "", -1)
  copyright := copyrightRegex.FindString(data)
  copyright = mergeSpaces(copyright)
  if len([]rune(copyright)) < 10 || len([]rune(copyright)) > 40 {
    copyright = ""
  }

  return ExtractedData{
    URL:         url,
    ICP:         icp,
    TechSupport: techSupport,
    Copyright:   copyright,
  }
}

func mergeSpaces(text string) string {
  // 创建一个正则表达式，用于匹配一个或多个换行符、制表符或空格
  re := regexp.MustCompile(`(\n{2,})|(\t{2,})|( {2,})`)

  // 使用正则表达式替换多个换行符、制表符或空格为一个换行符、制表符或空格
  result := re.ReplaceAllStringFunc(text, func(matched string) string {
    if matched[0] == '\n' {
      return "\n"
    } else if matched[0] == '\t' {
      return "\t"
    }
    return " "
  })

  return result
}

/*
func main() {
  // 创建浏览器上下文
  ctx, cancel := chromedp.NewContext(context.Background(), chromedp.WithLogf(log.Printf))
  defer cancel()

  // 设置超时时间
  ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
  defer cancel()

  // 要提取的网址
  url := "http://0x666.club/"

  // 提取网页底部数据
  var data string
  err := chromedp.Run(ctx,
    chromedp.Navigate(url),
    chromedp.WaitVisible(`body`, chromedp.ByQuery),
    chromedp.Evaluate(`
    function findFooterElement() {
      const visibleElements = Array.from(document.querySelectorAll('div'))
        .filter(element => getComputedStyle(element).display !== 'none' && getComputedStyle(element).visibility !== 'hidden');
      let lastVisibleElement = visibleElements.find(element => {
        const text = element.textContent;
        return /(?:\w|[\u4e00-\u9fa5])*ICP(备)?\d{1,10}(号)(-\d{1,})?/.test(text) ||
          /(技术支持|技术提供|网站建设|技术服务)[:：\s]*([A-Za-z0-9\u4e00-\u9fa5\(\)-]+)/.test(text) ||
          /(?:.*?)(?:copyright|©|版权).+?([\u4e00-\u9fa5]+(?:（[^）]+）)?)/i.test(text);
      });
      if (lastVisibleElement && lastVisibleElement.textContent.length > 300) {
        return visibleElements.find(element => element.id.includes('foot') || element.className.includes('foot'));
      }
      return lastVisibleElement;
    }
    const footerElement = findFooterElement();
    footerElement ? footerElement.textContent : '';
    `, &data),
  )

  if err != nil {
    log.Fatal(err)
  }
  icpRegex := regexp.MustCompile(`(?:\w|\p{Han})*ICP(备)?\d{1,10}(号)(-\d{1,})?`)
  techSupportRegex := regexp.MustCompile(`(?m)(技术支持|技术提供|网站建设|技术服务)[:：\s]*([A-Za-z0-9\x{4e00}-\x{9fa5}\(\)-]+)`)
  copyrightRegex := regexp.MustCompile(`(?i)(?:.*?)(?:copyright|©|版权).+?([\x{4e00}-\x{9fa5}]+)(?:.+?©.+?[\x{4e00}-\x{9fa5}]+)?`)

  icp := icpRegex.FindString(data)
  icp = mergeSpaces(icp)
  if len([]rune(icp)) < 10 || len([]rune(icp)) > 40 {
    icp = ""
  }
  data = strings.Replace(data, icp, "", -1)
  data = strings.Replace(data, "版权声明", "", -1)

  techSupport := techSupportRegex.FindString(data)
  techSupport = mergeSpaces(techSupport)
  if len([]rune(techSupport)) < 10 || len([]rune(techSupport)) > 40 {
    techSupport = ""
  }
  data = strings.Replace(data, techSupport, "", -1)
  copyright := copyrightRegex.FindString(data)
  copyright = mergeSpaces(copyright)
  if len([]rune(copyright)) < 10 || len([]rune(copyright)) > 40 {
    copyright = ""
  }

  extractedData := ExtractedData{
    ICP:         icp,
    TechSupport: techSupport,
    Copyright:   copyright,
  }

  // 输出提取到的数据
  jsonData, err := json.MarshalIndent(extractedData, "", "  ")
  if err != nil {
    log.Fatal(err)
  }
  fmt.Println("Extracted data:", string(jsonData))
}
func mergeSpaces(text string) string {
  // 创建一个正则表达式，用于匹配一个或多个换行符、制表符或空格
  re := regexp.MustCompile(`(\n{2,})|(\t{2,})|( {2,})`)

  // 使用正则表达式替换多个换行符、制表符或空格为一个换行符、制表符或空格
  result := re.ReplaceAllStringFunc(text, func(matched string) string {
    if matched[0] == '\n' {
      return "\n"
    } else if matched[0] == '\t' {
      return "\t"
    }
    return " "
  })

  return result
}


*/
